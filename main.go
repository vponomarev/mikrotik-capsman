package main

import (
	"flag"
	log "github.com/sirupsen/logrus"
	"gopkg.in/routeros.v2"
	"time"
)

var (
	// CapsMan configuration
	cmAddress = flag.String("address", "192.168.1.1:8728", "Addr/port for CapsMan")
	cmName    = flag.String("username", "admin", "Username for CapsMan")
	cmPass    = flag.String("password", "admin", "Password for CapsMan")

	// DHCP Server configuration
	dhcpAddr           = flag.String("dhcp-address", "192.168.1.1:8728", "Addr/port for DHCP Server")
	dhcpName           = flag.String("dhcp-username", "admin", "Username for DHCP Server")
	dhcpPass           = flag.String("dhcp-password", "admin", "Password for DHCP Server")
	dhcpReloadInterval = flag.Duration("dhcp-reload-interval", 60*time.Second, "Interval for DHCP reload")

	// HTTP Listen port
	listen = flag.String("listen", "0.0.0.0:8080", "HTTP Listen configuration")

	// Polling interval
	interval = flag.Duration("interval", 3*time.Second, "CapsMan Polling Interval")
)

func main() {
	flag.Parse()

	log.Warning("Starting Mikrotik CapsMan monitor daemon")
	//	log.WithFields(log.Fields{ "type": "smpp-lb",
	//	}).Warning("Override LogLevel to: ", l.String())

	// TODO: Add reconnect to CapsMAN in case of any failure

	l, err := GetDHCPLeases(*dhcpAddr, *dhcpName, *dhcpPass)
	if err != nil {
		log.WithFields(log.Fields{"dhcp-addr": *dhcpAddr, "dhcp-username": *dhcpName}).Fatal("Cannot connect to DHCP Server")
	}

	leaseList.RLock()
	leaseList.List = l
	leaseList.RUnlock()

	log.WithFields(log.Fields{"dhcp-addr": *dhcpAddr, "count": len(l)}).Info("Loaded DHCP Lease list")

	c, err := routeros.Dial(*cmAddress, *cmName, *cmPass)
	if err != nil {
		log.WithFields(log.Fields{"address": *cmAddress, "username": *cmName}).Fatal("Cannot connect to CapsMan node")
	}
	log.WithFields(log.Fields{"address": *cmAddress}).Info("Connected to CapsMan server")

	go serveHTTP()
	go reloadDHCP()

	// Run loop : scan Registration-Table
	RTLoop(*c)
}
