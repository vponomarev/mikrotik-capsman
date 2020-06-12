package main

import (
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
	"gopkg.in/routeros.v2"
	"os"
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

	// Optional configuration file
	configFileName = flag.String("config", "", "Configuration file name")
)

func main() {
	// Check for `--help` param
	if len(os.Args) > 1 {
		if os.Args[1] == "--help" {
			usage()
			return
		}
	}

	flag.Parse()

	log.SetLevel(log.DebugLevel)
	log.Warning("Starting Mikrotik CapsMan monitor daemon")
	//	log.WithFields(log.Fields{ "type": "smpp-lb",
	//	}).Warning("Override LogLevel to: ", l.String())

	// Load config if specified
	if *configFileName != "" {
		c, err := loadConfig(*configFileName)
		if err != nil {
			log.WithFields(log.Fields{"config": *configFileName}).Fatal("Error loading config file")
		}
		log.WithFields(log.Fields{"config": *configFileName}).Warn("Loaded config file")
		config = c
	}
	fmt.Println(config)

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
	RTLoop(*c, &config)
}
