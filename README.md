# Mikrotik-CapsMan
Web UI for Mikrotik CapsMan interface.

UI generates a dedicated and periodically updated WEB page with list of WiFi clients, that are connected to CapsMan. List is filled with extra information from Mikrotik DHCP Server.

UI contains:
- CapsMan interface name
- SSID
- Client MAC address
- Signal strength level
- Hostname (from DHCP)
- Comment (from DHCP)

Supported configuration options:
- `-address` - IP address and port of CapsMan server (default: `192.168.1.1:8728`)
- `-username` - Login for CapsMan server (default: `admin`)
- `-password` - Password for CaspMan server(default: none)
- `-dhcp-address` - IP address and port of DHCP server (default: `192.168.1.1:8728`)
- `-dhcp-username` - Login for DHCP server (default: `admin`)
- `-password` - Password for DHCP server(default: none)
- `-listen` - IP address and port for WEB UI server (default: `0.0.0.0:8080`)
- `-interval` - CapsMan server polling interval (default: `3 second`)

WEB UI is published at: http://`listen`/

# Future plans
- Add configuration file with support of human-readable names for specific MAC addresses
- Add MQTT Support with publishing of WiFi client state for intergration with HomeAssistant or other smart house servers

Have any ideas?
Feel free to send change requests.