// internal/order/tracking_ws.go
package order

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// ─────────────────────────────────────────────────────────────────────────────
// Upgrader
// ─────────────────────────────────────────────────────────────────────────────
var upgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// ─────────────────────────────────────────────────────────────────────────────
// Hub — order_id → connected clients
// ─────────────────────────────────────────────────────────────────────────────
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*websocket.Conn]bool // orderID → set of conns
}

var GlobalHub = &Hub{
	clients: make(map[string]map[*websocket.Conn]bool),
}

func (h *Hub) register(orderID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[orderID] == nil {
		h.clients[orderID] = make(map[*websocket.Conn]bool)
	}
	h.clients[orderID][conn] = true
}

func (h *Hub) unregister(orderID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients[orderID], conn)
	if len(h.clients[orderID]) == 0 {
		delete(h.clients, orderID)
	}
}

// BroadcastStatus — order service ya admin panel se call karo jab status change ho
func (h *Hub) BroadcastStatus(orderID string, payload TrackingPayload) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws marshal error: %v", err)
		return
	}

	for conn := range h.clients[orderID] {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("ws write error: %v", err)
			conn.Close()
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TrackingPayload — Flutter ko yahi JSON milega
// ─────────────────────────────────────────────────────────────────────────────
type TrackingPayload struct {
	OrderID     string   `json:"order_id"`
	Status      string   `json:"status"`
	DriverName  string   `json:"driver_name,omitempty"`
	DriverPhone string   `json:"driver_phone,omitempty"`
	DriverImage string   `json:"driver_image,omitempty"`
	DriverLat   *float64 `json:"driver_lat,omitempty"`
	DriverLng   *float64 `json:"driver_lng,omitempty"`
	ETA         int      `json:"eta_minutes,omitempty"`
	Message     string   `json:"message,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Handler — GET /orders/track/ws/{order_id}
// main.go mein public subrouter pe register karo (auth bypass)
// ya protected pe — dono kaam karega
// ─────────────────────────────────────────────────────────────────────────────
func (s *Service) TrackWebSocket(w http.ResponseWriter, r *http.Request) {
	orderID := mux.Vars(r)["order_id"]
	if orderID == "" {
		http.Error(w, "order_id required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}
	defer conn.Close()

	GlobalHub.register(orderID, conn)
	defer GlobalHub.unregister(orderID, conn)

	log.Printf("✅ WS connected: order=%s", orderID)

	// ── Turant current status bhejo ────────────────────────────────────────
	tracking, err := s.Track(orderID)
	if err == nil {
		payload := buildPayload(orderID, tracking)
		data, _ := json.Marshal(payload)
		conn.WriteMessage(websocket.TextMessage, data)
	}

	// ── Ping loop — connection alive rakhne ke liye ────────────────────────
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	done := make(chan struct{})

	// Read loop (client disconnect detect karne ke liye)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				close(done)
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			log.Printf("🔌 WS disconnected: order=%s", orderID)
			return
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper
// ─────────────────────────────────────────────────────────────────────────────
func buildPayload(orderID string, data map[string]interface{}) TrackingPayload {
	p := TrackingPayload{OrderID: orderID}

	if v, ok := data["status"].(string); ok {
		p.Status = v
	}
	if v, ok := data["driver_name"].(string); ok {
		p.DriverName = v
	}
	if v, ok := data["driver_phone"].(string); ok {
		p.DriverPhone = v
	}
	if v, ok := data["driver_image"].(string); ok {
		p.DriverImage = v
	}
	if v, ok := data["driver_lat"].(*float64); ok {
		p.DriverLat = v
	}
	if v, ok := data["driver_lng"].(*float64); ok {
		p.DriverLng = v
	}
	return p
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateOrderStatus — jab bhi status change ho, yahan se broadcast karo
// Order service ke Cancel ya kisi admin handler se call karo:
//   GlobalHub.BroadcastStatus(orderID, TrackingPayload{...})
// ─────────────────────────────────────────────────────────────────────────────