# Mikrotik-CapsMan
Web UI for Mikrotik CapsMan/WiFi interface.

![UI Example](https://github.com/vponomarev/mikrotik-capsman/raw/master/doc/mikrotik-capsman-ui-sample-processed.PNG)

UI generates a dedicated and periodically updated WEB page with list of WiFi clients, that are connected to CapsMan. List is filled with extra information from Mikrotik DHCP Server.

UI contains:
- CapsMan interface name
- SSID
- Client MAC address
- Client IP address (from DHCP)
- Signal strength level
- Hostname (from DHCP)
- Comment (from DHCP)

Supported configuration params:
- `-config` - Name of configuration file (default: `config.yml`, example: `-config config-custom.yml`)

Supported parameters in configuration file:
- Router configuration
  - `mode` - operation mode (CapsMan / WiFi)
  - `address` - IP address and port of API interface (normally `8728`)
  - `username` - Login for API connection
  - `password` - Password for API connection
  - `interval` - Polling interval (examples: `5s`, `1m`, ...)
- DHCP Server configuration (only Mikrotik DHCP server is supported), optional
  - `address` - IP address and port of API interface (normally `8728`)
  - `username` - Login for API connection
  - `password` - Password for API connection
  - `interval` - Polling interval (examples: `5s`, `1m`, ...)
- Device list (personal configuration for each device)
  - `name` - Name, that will be displayed in interface
  - `mac` - MAC address of this device
  - `on.connect` - Action for connect event
  - `on.disconnect` - Action for disconnect event
  - `on.roaming` - Action for roaming between AP's (for CapsMan mode)
  - `on.level` - Action for signal level change

Each `on.*` event have the following configuration fields:
- `http.post` - URL for HTTP Post request with template support
- `http.get` - URL for HTTP Get request with templates (will be used if there is no `http.post` line)
- `http.post.content` - Content for HTTP Post request
- `http.header` - List of HTTP headers, that should be added into request (can be used for authentification/configuration of content-type and so on)

Supported template variables for `http.post`, `http.get` and `http.post.content` fields:
- `name` - Name of device (from configuration)
- `mac` - MAC address of device
- `roaming.to` - During roaming, name of New AP
- `roaming.from` - During roaming, name of OLD AP
- `level.to` - During level change, value of new signal level
- `level.from` - During level change, value of old signal level
  

WEB UI is published at: http://`listen`/

# Future plans
- Add configuration file with support of human-readable names for specific MAC addresses
- Add MQTT Support with publishing of WiFi client state for intergration with HomeAssistant or other smart house servers

Have any ideas?
Feel free to send change requests.