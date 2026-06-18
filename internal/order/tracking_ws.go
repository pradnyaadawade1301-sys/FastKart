package order

import (
    "encoding/json"
    "log"
    "net/http"
    "strings"
    "sync"
    "time"

    "github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
    CheckOrigin:     func(r *http.Request) bool { return true },
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
}

type Hub struct {
    mu      sync.RWMutex
    clients map[string]map[*websocket.Conn]bool
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

func (h *Hub) BroadcastStatus(orderID string, payload TrackingPayload) {
    h.mu.RLock()
    defer h.mu.RUnlock()
    data, err := json.Marshal(payload)
    if err != nil {
        return
    }
    for conn := range h.clients[orderID] {
        if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
            conn.Close()
        }
    }
}

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

func (s *Service) TrackWebSocket(w http.ResponseWriter, r *http.Request) {
    parts := strings.Split(r.URL.Path, "/")
    orderID := parts[len(parts)-1]
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

    tracking, err := s.Track(orderID)
    if err == nil {
        payload := buildPayload(orderID, tracking)
        data, _ := json.Marshal(payload)
        conn.WriteMessage(websocket.TextMessage, data)
    }

    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    done := make(chan struct{})

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
            return
        case <-ticker.C:
            if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}

func buildPayload(orderID string, data map[string]interface{}) TrackingPayload {
    p := TrackingPayload{OrderID: orderID}
    if v, ok := data["status"].(string); ok {
        p.Status = v
    }
    if v, ok := data["driver_name"].(string); ok {
        p.DriverName = v
    }
    if v, ok := data["driver_lat"].(*float64); ok {
        p.DriverLat = v
    }
    if v, ok := data["driver_lng"].(*float64); ok {
        p.DriverLng = v
    }
    return p
}
