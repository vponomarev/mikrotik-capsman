package main

import (
	"flag"
	log "github.com/sirupsen/logrus"
	"gopkg.in/routeros.v2"
	"os"
	"time"
)

var (
	// HTTP Listen port
	listen = flag.String("listen", "0.0.0.0:8080", "HTTP Listen configuration")

	// Polling interval
	interval = flag.Duration("interval", 3*time.Second, "CapsMan Polling Interval")

	// Optional configuration file
	configFileName = flag.String("config", "config.yml", "Configuration file name")
)

func main() {
	// Check for `--help` param
	if len(os.Args) > 1 {
		if os.Args[1] == "--help" {
			usage()
			return
		}
	}

	// Init broadcast data
	broadcastData.Init()
	go broadcastData.EventHandler()

	flag.Parse()

	log.SetLevel(log.DebugLevel)
	log.Warning("Starting Mikrotik CapsMan monitor daemon")

	// Load config if specified
	cfg, err := loadConfig(*configFileName)
	if err != nil {
		log.WithFields(log.Fields{"config": *configFileName}).Fatal("Error loading config file")
	}

	// Switch log level if required
	if cfg.Log.Level != log.DebugLevel {
		log.WithFields(log.Fields{"loglevel": cfg.Log.Level}).Warn("Switching Log Level")
		log.SetLevel(cfg.Log.Level)
	}

	// Validate reload interval duration
	if cfg.Capsman.Interval < (2 * time.Second) {
		log.WithFields(log.Fields{"config": *configFileName}).Fatal("capsman.interval is too short, minimum value is 2 sec")
	}
	if cfg.DHCP.Interval < (10 * time.Second) {
		log.WithFields(log.Fields{"config": *configFileName}).Fatal("dhcp.interval is too short, minimum value is 10 sec")
	}

	log.WithFields(log.Fields{"config": *configFileName}).Warn("Loaded config file")
	config = cfg

	l, err := GetDHCPLeases(config.DHCP.Address, config.DHCP.Username, config.DHCP.Password)
	if err != nil {
		log.WithFields(log.Fields{"dhcp-addr": config.DHCP.Address, "dhcp-username": config.DHCP.Username}).Fatal("Cannot connect to DHCP Server")
	}

	leaseList.RLock()
	leaseList.List = l
	leaseList.RUnlock()

	log.WithFields(log.Fields{"dhcp-addr": config.DHCP.Address, "count": len(l)}).Info("Loaded DHCP Lease list")

	conn, err := routeros.Dial(config.Capsman.Address, config.Capsman.Username, config.Capsman.Password)
	if err != nil {
		log.WithFields(log.Fields{"address": config.Capsman.Address, "username": config.Capsman.Username}).Fatal("Cannot connect to CapsMan node")
	}
	log.WithFields(log.Fields{"address": config.Capsman.Address}).Info("Connected to CapsMan server")

	go serveHTTP()
	go reloadDHCP()

	// Run loop : scan Registration-Table
	RTLoop(*conn, &config)
}
