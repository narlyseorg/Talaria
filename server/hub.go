package server

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients map[*Client]bool

	register chan *Client

	unregister chan *Client

	incoming chan []byte

	ticker *time.Ticker
	quit   chan struct{}

	mu sync.RWMutex
}

type Client struct {
	hub *Hub

	conn *websocket.Conn

	send chan *websocket.PreparedMessage
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		incoming:   make(chan []byte, 16),
		clients:    make(map[*Client]bool),
		ticker:     time.NewTicker(1 * time.Second),
		quit:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	defer func() {
		h.ticker.Stop()
	}()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case msg := <-h.incoming:

			var cmd struct {
				Action string `json:"action"`
				Rate   int    `json:"rate"` // milliseconds
			}
			if err := json.Unmarshal(msg, &cmd); err == nil {
				switch cmd.Action {
				case "set_rate":

					if cmd.Rate >= 250 && cmd.Rate <= 10000 {
						h.ticker.Reset(time.Duration(cmd.Rate) * time.Millisecond)
						log.Printf("Refresh rate changed to %dms", cmd.Rate)
					}
				}
			}

		case <-h.ticker.C:

			h.mu.RLock()
			count := len(h.clients)
			h.mu.RUnlock()

			if count > 0 {
				metrics := CollectAll(count)
				data, err := json.Marshal(metrics)
				if err != nil {
					log.Printf("JSON marshal error: %v", err)
					continue
				}

				pm, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
				if err != nil {
					log.Printf("PreparedMessage error: %v", err)
					continue
				}

				h.mu.Lock()
				for client := range h.clients {
					select {
					case client.send <- pm:
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
				h.mu.Unlock()
			}

		case <-h.quit:
			return
		}
	}
}

func (h *Hub) Stop() {
	close(h.quit)

	h.mu.Lock()
	for client := range h.clients {

		if uc := client.conn.UnderlyingConn(); uc != nil {
			if tc, ok := uc.(*net.TCPConn); ok {
				tc.SetLinger(0)
			}
		}
		client.conn.Close()
	}
	h.mu.Unlock()
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
