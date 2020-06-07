package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"gopkg.in/routeros.v2"
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

var broadcastData BroadcastData
var leaseList LeaseList

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
	ticker := time.NewTicker(*dhcpReloadInterval)
	for {
		select {
		case <-ticker.C:
			l, err := GetDHCPLeases(*dhcpAddr, *dhcpName, *dhcpPass)
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

func RTLoop(c routeros.Client) {
	for {
		reply, err := c.Run("/caps-man/registration-table/print")
		if err != nil {
			log.WithFields(log.Fields{"address": *cmAddress, "username": *cmName}).Error("Error during request to CapsMan server: ", err)

			// Try to close connection
			c.Close()

			// Reconnect loop
			for {
				// Sleep for 5 sec
				time.Sleep(5 * time.Second)
				cNew, err := routeros.Dial(*cmAddress, *cmName, *cmPass)
				if err != nil {
					log.WithFields(log.Fields{"address": *cmAddress, "username": *cmName}).Error("Reconnect error to CapsMan server: ", err)
					continue
				}
				c = *cNew
				log.WithFields(log.Fields{"address": *cmAddress, "username": *cmName}).Warn("Reconnected to CapsMan server")
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

		output, err := json.Marshal(report)
		if err != nil {
			log.Fatal("Error JSON MARSHAL: ", err)
			return
		}

		broadcastData.RLock()
		broadcastData.Data = string(output)
		broadcastData.LastUpdate = time.Now()
		broadcastData.RUnlock()

		//		fmt.Print("\n")

		time.Sleep(*interval)
	}
}
