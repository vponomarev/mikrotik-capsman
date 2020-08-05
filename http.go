package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"html/template"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

func serveHTTP() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fn := "html/index.html"
		t, err := template.ParseFiles(fn)
		if err != nil {
			fmt.Fprint(w, "Error parsing template file:", fn, " with error:", err)
			return
		}
		if t.Execute(w, map[string]string{"ServerHost": r.Host}) != nil {
			fmt.Fprint(w, "Internal error: cannot execute template")
		}
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

	log.WithFields(log.Fields{"listen": *listen}).Warn("Starting HTTP Listener")
	err := http.ListenAndServe(*listen, nil)
	log.WithFields(log.Fields{"listen": *listen}).Fatal("Received an error from HTTP Listener: ", err)
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
		case <-pingTicker.C:
			if ws.SetWriteDeadline(time.Now().Add(writeWait)) != nil {
				return
			}
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		case <-dataTicker.C:
			// Check for LastUpdate
			broadcastData.RLock()
			if broadcastData.LastUpdate.After(lastUpdate) {
				data := broadcastData.Data
				lastUpdate = broadcastData.LastUpdate
				broadcastData.RUnlock()

				if ws.SetWriteDeadline(time.Now().Add(writeWait)) != nil {
					return
				}
				if err := ws.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
					return
				}
			} else {
				broadcastData.RUnlock()
			}
		}
	}
}

func WSreader(ws *websocket.Conn) {
	defer ws.Close()
	ws.SetReadLimit(512)

	if ws.SetReadDeadline(time.Now().Add(pongWait)) != nil {
		return
	}
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
	}
}

func makeRequest(event ConfigEvent, params map[string]string) {

	// Prepare request params
	method := "GET"
	data := ""
	url := event.HttpGet
	if len(event.HttpPost) > 0 {
		method = "POST"
		url = event.HttpPost
		data = event.HttpPostContent
	}

	for k, v := range params {
		url = strings.ReplaceAll(url, "{"+k+"}", v)
		data = strings.ReplaceAll(data, "{"+k+"}", v)
	}

	// Prepare request
	client := &http.Client{}
	req, err := http.NewRequest(method, url, strings.NewReader(data))
	if err != nil {
		log.WithFields(log.Fields{"action": "notify", "url": url}).Info("Error creating HTTP request: ", err)
		return
	}

	// Add headers
	for k, v := range event.HttpHeader {
		req.Header.Add(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{"action": "notify", "method": method, "url": url, "state": "fail"}).Info("Error making HTTP request: ", err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.WithFields(log.Fields{"action": "notify", "method": method, "url": url, "state": "fail"}).Info("Error reading body of HTTP request: ", err)
		return
	}
	log.WithFields(log.Fields{"action": "notify", "method": method, "url": url, "state": "ok", "resp-body-len": len(body)}).Debug("HTTP Notification is sent")
}
