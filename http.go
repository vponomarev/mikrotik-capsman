package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"html/template"
	"net/http"
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
		t.Execute(w, map[string]string{"ServerHost": r.Host})
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

	log.WithFields(log.Fields{"listen": *listen}).Fatal("Starting HTTP Listener")
	err := http.ListenAndServe(*listen, nil)
	log.WithFields(log.Fields{"listen": *listen}).Warn("Received an error from HTTP Listener: ", err)
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
		case <-pingTicker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
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
