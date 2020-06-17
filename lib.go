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

var WS = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type BroadcastData struct {
	Data       string
	LastUpdate time.Time
	sync.RWMutex
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

type Config struct {
	Capsman ConfMikrotik `yaml:"capsman"`
	DHCP    ConfMikrotik `yaml:"dhcp"`
	Devices []ConfDevice `yaml:"devices"`
	sync.RWMutex
}

var broadcastData BroadcastData
var leaseList LeaseList
var config Config
var devNames map[string]string

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
	for {
		select {
		case <-ticker.C:
			l, err := GetDHCPLeases(config.DHCP.Address, config.DHCP.Username, config.DHCP.Password)
			if err != nil {
				log.Fatal("Cannot connect to DHCP Server for reload: ", err)
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

func RTLoop(c routeros.Client, conf *Config) {
	for {
		reply, err := c.Run("/caps-man/registration-table/print")
		if err != nil {
			log.WithFields(log.Fields{"address": config.Capsman.Address, "username": config.Capsman.Username}).Error("Error during request to CapsMan server: ", err)

			// Try to close connection
			c.Close()

			// Reconnect loop
			for {
				// Sleep for 5 sec
				time.Sleep(5 * time.Second)
				cNew, err := routeros.Dial(config.Capsman.Address, config.Capsman.Username, config.Capsman.Password)
				if err != nil {
					log.WithFields(log.Fields{"address": config.Capsman.Address, "username": config.Capsman.Username}).Error("Reconnect error to CapsMan server: ", err)
					continue
				}
				c = *cNew
				log.WithFields(log.Fields{"address": config.Capsman.Address, "username": config.Capsman.Username}).Warn("Reconnected to CapsMan server")
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
			rec := ReportEntry{
				IP:        ip,
				Name:      devNames[re.Map["mac-address"]],
				Interface: re.Map["interface"],
				SSID:      re.Map["ssid"],
				MAC:       re.Map["mac-address"],
				Signal:    re.Map["rx-signal"],
				Hostname:  n,
				Comment:   c,
			}
			report = append(report, rec)

			// fmt.Printf("%-20s\t%-20s\t%-20s\t%-10s\t%-30s\t%-30s\n", re.Map["interface"], re.Map["ssid"], re.Map["mac-address"], re.Map["rx-signal"], n, c)
		}
		log.WithFields(log.Fields{"count": len(report)}).Debug("Reloaded CapsMan entries")
		leaseList.RUnlock()

		if err = reportUpdate(report); err != nil {
			log.WithFields(log.Fields{}).Warn("Error during reportUpdate: ", err)

		}
		/*
			output, err := json.Marshal(report)
			if err != nil {
				log.Fatal("Error JSON MARSHAL: ", err)
				return
			}

			broadcastData.RLock()
			broadcastData.Data = string(output)
			broadcastData.LastUpdate = time.Now()
			broadcastData.RUnlock()
		*/
		//		fmt.Print("\n")

		time.Sleep(*interval)
	}
}

func loadConfig(configFileName string) (config Config, err error) {
	config = Config{}
	devNames = make(map[string]string)

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
		devNames[strings.ToUpper(v.MAC)] = v.Name
	}

	return
}

func usage() {

}

// Handle report update request
func reportUpdate(report []ReportEntry) error {
	output, err := json.Marshal(report)
	if err != nil {
		return err
	}

	broadcastData.RLock()
	broadcastData.Data = string(output)
	broadcastData.LastUpdate = time.Now()
	broadcastData.RUnlock()

	return nil
}
