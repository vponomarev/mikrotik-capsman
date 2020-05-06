package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"gopkg.in/routeros.v2"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	dhcpAddr = flag.String("dhcp-cmAddress", "192.168.1.1:8728", "Addr/port for DHCP Server")
	dhcpName = flag.String("dhcp-username", "admin", "Username for DHCP Server")
	dhcpPass = flag.String("dhcp-password", "admin", "Password for DHCP Server")

	cmAddress	= flag.String("address", "192.168.1.2:8728", "Addr/port for CapsMan")
	cmName   	= flag.String("username", "admin", "Username for CapsMan")
	cmPass   	= flag.String("password", "admin", "Password for CapsMan")

	listen		= flag.String("listen", "0.0.0.0:8080", "HTTP Listen configuration")

	interval   = flag.Duration("interval", 3*time.Second, "Interval")
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
	IP			string
	MAC			string
	Server		string
	Hostname	string
	Comment		string
}

type ReportEntry struct {
	Interface		string
	SSID			string
	MAC				string
	Signal			string
	Hostname		string
	Comment			string
}

var WS = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type BroadcastData struct {
	Data			string
	LastUpdate		time.Time
	sync.RWMutex
}

var broadcastData BroadcastData

func GetDHCPLeases(address, username, password string) (list []LeaseEntry, err error){
	c, err := routeros.Dial(address, username, password)
	if err != nil {
		return
	}

	reply, err := c.Run("/ip/dhcp-server/lease/print")
	if err != nil {
		return
	}

	for _, re := range reply.Re {
		list = append(list, LeaseEntry{
			IP:      	re.Map["address"],
			MAC:     	re.Map["mac-address"],
			Server:  	re.Map["server"],
			Hostname:	re.Map["host-name"],
			Comment: 	re.Map["comment"],
		})
	}
	return
}

func FindLeaseByMAC(list []LeaseEntry, mac string) (e LeaseEntry, ok bool) {
	for _, e := range list {
		if e.MAC == mac {
			return e, true
		}
	}
	return
}

func serveHTTP() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "html/index.html")
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := WS.Upgrade(w, r, nil)
		if err != nil {
			if _, ok := err.(websocket.HandshakeError); !ok {
				log.Println(err)
			}
			return
		}

		go WSwriter(conn)
		WSreader(conn)

	})
	http.ListenAndServe(*listen, nil)
}

func WSreader(ws *websocket.Conn) {
	defer ws.Close()
	ws.SetReadLimit(512)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
	}
}

func WSwriter(ws *websocket.Conn) {
	pingTicker := time.NewTicker(pingPeriod)
	dataTicker := time.NewTicker(100 * time.Millisecond)

	var lastUpdate time.Time

	defer func() {
		pingTicker.Stop()
		dataTicker.Stop()
		ws.Close()
	}()

	for {
		select {
		case <- pingTicker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		case <- dataTicker.C:
			// Check for LastUpdate
			broadcastData.RLock()
			if broadcastData.LastUpdate.After(lastUpdate) {
				data := broadcastData.Data
				lastUpdate = broadcastData.LastUpdate
				broadcastData.RUnlock()

				ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
					return
				}
			} else {
				broadcastData.RUnlock()
			}
		}
	}
}


func main() {
	flag.Parse()

	leaseList, err := GetDHCPLeases(*dhcpAddr, *dhcpName, *dhcpPass)
	if err != nil {
		log.Fatal("Cannot connect to DHCP Server: ", err)
	}
	fmt.Println(leaseList)

	c, err := routeros.Dial(*cmAddress, *cmName, *cmPass)
	if err != nil {
		log.Fatal("Cannot connect to CapsMan node: ", err)
	}

	go serveHTTP()

	for {
		reply, err := c.Run("/caps-man/registration-table/print")
		if err != nil {
			log.Fatal("Error fetching CapsMan data: ", err)
		}

		var report []ReportEntry

		for _, re := range reply.Re {
			var n,c string
			if le, ok := FindLeaseByMAC(leaseList, re.Map["mac-address"]); ok {
				n = le.Hostname
				c = le.Comment
			}
			rec := ReportEntry{
				Interface: re.Map["interface"],
				SSID:      re.Map["ssid"],
				MAC:       re.Map["mac-address"],
				Signal:    re.Map["rx-signal"],
				Hostname:  n,
				Comment:   c,
			}
			report = append(report, rec)

			fmt.Printf("%-20s\t%-20s\t%-20s\t%-10s\t%-30s\t%-30s\n", re.Map["interface"], re.Map["ssid"], re.Map["mac-address"], re.Map["rx-signal"], n, c)
		}

		output, err := json.Marshal(report)
		if err != nil {
			log.Fatal("Error JSON MARSHAL: ", err)
			return
		}

		broadcastData.RLock()
		broadcastData.Data = string(output)
		broadcastData.LastUpdate = time.Now()
		broadcastData.RUnlock()

		fmt.Print("\n")

		time.Sleep(*interval)
	}
}

