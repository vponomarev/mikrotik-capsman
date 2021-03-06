package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"gopkg.in/routeros.v2"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"strings"
	"sync"
	"time"
)

const (
	// Time allowed to read the next pong message from the client.
	pongWait = 60 * time.Second

	// Send pings to client with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Time allowed to write the file to the client.
	writeWait = 10 * time.Second
)

// Event types
const (
	EVENT_CONNECT = iota
	EVENT_ROAMING
	EVENT_DISCONNECT
	EVENT_LEVEL
)

type LeaseEntry struct {
	IP       string
	MAC      string
	Server   string
	Hostname string
	Comment  string
}

type ReportEntry struct {
	IP        string
	Name      string
	Interface string
	SSID      string
	MAC       string
	Signal    string
	Hostname  string
	Comment   string
}

type ReportEvent struct {
	EventType int
	Old       ReportEntry
	New       ReportEntry
}

var WS = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type BroadcastData struct {
	Report     []ReportEntry
	ReportMap  map[string]ReportEntry
	Data       string
	LastUpdate time.Time
	sync.RWMutex

	ReportChan chan ReportEvent
}

type LeaseList struct {
	List []LeaseEntry
	sync.RWMutex
}

type ConfMikrotik struct {
	Address  string        `yaml:"address"`
	Username string        `yaml:"username"`
	Password string        `yaml:"password"`
	Interval time.Duration `yaml:"interval"`
	Mode     string        `yaml:"mode"`
}

type ConfDevice struct {
	Name         string      `yaml:"name"`
	MAC          string      `yaml:"mac"`
	OnConnect    ConfigEvent `yaml:"on.connect"`
	OnDisconnect ConfigEvent `yaml:"on.disconnect"`
	OnRoaming    ConfigEvent `yaml:"on.roaming"`
	OnLevel      ConfigEvent `yaml:"on.level"`
}

type ConfigEvent struct {
	HttpPost        string            `yaml:"http.post"`
	HttpGet         string            `yaml:"http.get"`
	HttpPostContent string            `yaml:"http.post.content"`
	HttpHeader      map[string]string `yaml:"http.header"`
}

type LogInfo struct {
	Level log.Level `yaml:"level"`
}

type Config struct {
	Log     LogInfo      `yaml:"log"`
	Router  ConfMikrotik `yaml:"router"`
	DHCP    ConfMikrotik `yaml:"dhcp"`
	Devices []ConfDevice `yaml:"devices"`
}

// Init BroadcastData entry
func (b *BroadcastData) Init() {
	b.ReportMap = map[string]ReportEntry{}
	b.ReportChan = make(chan ReportEvent)
}

var broadcastData BroadcastData
var leaseList LeaseList

var config Config
var configMTX sync.RWMutex

var devList map[string]ConfDevice
var devListMTX sync.RWMutex

func GetDHCPLeases(address, username, password string) (list []LeaseEntry, err error) {
	cl, err := routeros.Dial(address, username, password)
	if err != nil {
		return
	}
	defer cl.Close()

	reply, err := cl.Run("/ip/dhcp-server/lease/print")
	if err != nil {
		return
	}

	for _, re := range reply.Re {
		list = append(list, LeaseEntry{
			IP:       re.Map["address"],
			MAC:      re.Map["mac-address"],
			Server:   re.Map["server"],
			Hostname: re.Map["host-name"],
			Comment:  re.Map["comment"],
		})
	}
	return
}

func reloadDHCP() {
	ticker := time.NewTicker(config.DHCP.Interval)
	for { // nolint:gosimple
		select {
		case <-ticker.C:
			l, err := GetDHCPLeases(config.DHCP.Address, config.DHCP.Username, config.DHCP.Password)
			if err != nil {
				log.WithFields(log.Fields{"dhcp-addr": config.DHCP.Address}).Error("Error reloading DHCP Leases: ", err)
				return
			} else {
				leaseList.RLock()
				leaseList.List = l
				leaseList.RUnlock()
				log.WithFields(log.Fields{"count": len(l)}).Debug("Reloaded DHCP Leases")
			}

		}
	}
}

func FindLeaseByMAC(list []LeaseEntry, mac string) (e LeaseEntry, ok bool) {
	for _, e := range list {
		if e.MAC == mac {
			return e, true
		}
	}
	return
}

func RTLoop(c *routeros.Client, conf *Config) {
	for {
		cmd := "/caps-man/registration-table/print"
		if strings.ToLower(config.Router.Mode) == "wifi" {
			cmd = "/interface/wireless/registration-table/print"
		}

		reply, err := c.Run(cmd)
		if err != nil {
			log.WithFields(log.Fields{"address": config.Router.Address, "username": config.Router.Username}).Error("Error during request to CapsMan server: ", err)

			// Try to close connection
			c.Close()

			// Reconnect loop
			for {
				// Sleep for 5 sec
				time.Sleep(5 * time.Second)
				cNew, err := routeros.Dial(config.Router.Address, config.Router.Username, config.Router.Password)
				if err != nil {
					log.WithFields(log.Fields{"address": config.Router.Address, "username": config.Router.Username}).Error("Reconnect error to CapsMan server: ", err)
					continue
				}
				c = cNew
				log.WithFields(log.Fields{"address": config.Router.Address, "username": config.Router.Username}).Warn("Reconnected to CapsMan server")
				break
			}
			continue
		}

		var report []ReportEntry

		leaseList.RLock()
		for _, re := range reply.Re {
			var n, c, ip string
			if le, ok := FindLeaseByMAC(leaseList.List, re.Map["mac-address"]); ok {
				n = le.Hostname
				c = le.Comment
				ip = le.IP
			}
			devListMTX.RLock()
			rec := ReportEntry{
				IP:        ip,
				Name:      devList[re.Map["mac-address"]].Name,
				Interface: re.Map["interface"],
				SSID:      re.Map["ssid"],
				MAC:       re.Map["mac-address"],
				Signal:    re.Map["rx-signal"],
				Hostname:  n,
				Comment:   c,
			}

			if strings.ToLower(config.Router.Mode) == "wifi" {
				rec.Signal = re.Map["signal-strength"]
				if i := strings.Index(rec.Signal, "@"); i > 0 {
					rec.Signal = rec.Signal[0:i]
				}
			}
			devListMTX.RUnlock()
			report = append(report, rec)

			// fmt.Printf("%-20s\t%-20s\t%-20s\t%-10s\t%-30s\t%-30s\n", re.Map["interface"], re.Map["ssid"], re.Map["mac-address"], re.Map["rx-signal"], n, c)
		}
		log.WithFields(log.Fields{"count": len(report)}).Debug("Reloaded CapsMan entries")
		leaseList.RUnlock()

		if err = broadcastData.reportUpdate(report); err != nil {
			log.WithFields(log.Fields{}).Warn("Error during reportUpdate: ", err)

		}

		time.Sleep(*interval)
	}
}

func loadConfig(configFileName string) (config Config, err error) {
	devListMTX.RLock()
	defer devListMTX.RUnlock()

	config = Config{}
	devList = make(map[string]ConfDevice)

	source, err := ioutil.ReadFile(configFileName)
	if err != nil {
		err = fmt.Errorf("cannot read config file [%s]", configFileName)
		return
	}

	if err = yaml.Unmarshal(source, &config); err != nil {
		err = fmt.Errorf("error parsing config file [%s]: %v", configFileName, err)
		return
	}

	for _, v := range config.Devices {
		devList[strings.ToUpper(v.MAC)] = v
	}

	return
}

func usage() {

}

// Handle report update request
func (b *BroadcastData) reportUpdate(report []ReportEntry) error {
	output, err := json.Marshal(report)
	if err != nil {
		return err
	}

	// Lock mutex
	b.RLock()
	defer b.RUnlock()

	// Prepare new list of entries
	rm := map[string]ReportEntry{}
	for _, v := range report {
		rm[v.MAC] = v
	}

	// Scan for new entries
	for k := range rm {
		if _, ok := b.ReportMap[k]; !ok {
			// New entry
			b.ReportChan <- ReportEvent{
				EventType: EVENT_CONNECT,
				New:       rm[k],
			}
		} else {
			// Check for roaming
			if rm[k].Interface != b.ReportMap[k].Interface {
				b.ReportChan <- ReportEvent{
					EventType: EVENT_ROAMING,
					Old:       b.ReportMap[k],
					New:       rm[k],
				}
			}

			// Check for signal level change
			if rm[k].Signal != b.ReportMap[k].Signal {
				b.ReportChan <- ReportEvent{
					EventType: EVENT_LEVEL,
					Old:       b.ReportMap[k],
					New:       rm[k],
				}
			}
		}
	}

	// Scan for deleted entries
	for k := range b.ReportMap {
		if _, ok := rm[k]; !ok {
			b.ReportChan <- ReportEvent{
				EventType: EVENT_DISCONNECT,
				Old:       b.ReportMap[k],
			}
		}
	}

	b.ReportMap = rm
	b.Report = report
	b.Data = string(output)
	b.LastUpdate = time.Now()

	return nil
}

func (b *BroadcastData) EventHandler() {
	for { // nolint:gosimple
		select {
		case data := <-b.ReportChan:
			// fmt.Printf("New event received: %v\n", data)
			switch data.EventType {
			case EVENT_CONNECT:
				log.WithFields(log.Fields{"action": "register", "mac": data.New.MAC, "name": data.New.Name, "interface": data.New.Interface, "ssid": data.New.SSID, "hostname": data.New.Hostname, "comment": data.New.Comment, "level-to": data.New.Signal}).Info("New connection registered")

				// Get device info
				devListMTX.RLock()
				dev, ok := devList[data.New.MAC]
				devListMTX.RUnlock()
				if ok {
					if (len(dev.OnConnect.HttpPost) > 0) || (len(dev.OnConnect.HttpGet) > 0) {
						go makeRequest(dev.OnConnect, map[string]string{
							"name":         dev.Name,
							"mac":          data.New.MAC,
							"roaming.to":   "",
							"roaming.from": "",
							"level.to":     data.New.Signal,
							"level.from":   "",
						})
					}
				}

			case EVENT_DISCONNECT:
				log.WithFields(log.Fields{"action": "disconnect", "mac": data.Old.MAC, "name": data.Old.Name, "interface": data.Old.Interface, "hostname": data.Old.Hostname, "comment": data.Old.Comment}).Info("Client disconnect")

				// Get device info
				devListMTX.RLock()
				dev, ok := devList[data.New.MAC]
				devListMTX.RUnlock()
				if ok {
					if (len(dev.OnDisconnect.HttpPost) > 0) || (len(dev.OnDisconnect.HttpGet) > 0) {
						go makeRequest(dev.OnDisconnect, map[string]string{
							"name":         dev.Name,
							"mac":          data.Old.MAC,
							"roaming.to":   "",
							"roaming.from": "",
							"level.to":     "",
							"level.from":   data.Old.Signal,
						})
					}
				}

			case EVENT_ROAMING:
				log.WithFields(log.Fields{"action": "roaming", "mac": data.New.MAC, "name": data.New.Name, "interface-from": data.Old.Interface, "interface-to": data.New.Interface, "level-from": data.Old.Signal, "level-to": data.New.Signal}).Info("Client roaming")

				// Get device info
				devListMTX.RLock()
				dev, ok := devList[data.New.MAC]
				devListMTX.RUnlock()
				if ok {
					if (len(dev.OnRoaming.HttpPost) > 0) || (len(dev.OnRoaming.HttpGet) > 0) {
						go makeRequest(dev.OnRoaming, map[string]string{
							"name":         dev.Name,
							"mac":          data.New.MAC,
							"roaming.to":   data.New.Interface,
							"roaming.from": data.Old.Interface,
							"level.from":   data.Old.Signal,
							"level.to":     data.New.Signal,
						})
					}
				}

			case EVENT_LEVEL:
				log.WithFields(log.Fields{"action": "level", "mac": data.New.MAC, "name": data.New.Name, "interface": data.New.Interface, "level-from": data.Old.Signal, "level-to": data.New.Signal}).Debug("Signal level change")

				// Get device info
				devListMTX.RLock()
				dev, ok := devList[data.New.MAC]
				devListMTX.RUnlock()
				if ok {
					if (len(dev.OnLevel.HttpPost) > 0) || (len(dev.OnLevel.HttpGet) > 0) {
						go makeRequest(dev.OnLevel, map[string]string{
							"name":         dev.Name,
							"mac":          data.Old.MAC,
							"roaming.to":   "",
							"roaming.from": "",
							"level.from":   data.Old.Signal,
							"level.to":     data.New.Signal,
						})
					}
				}

			default:

			}
		}
	}
}
