package handlers

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// HealthAlertMessage represents a health factor alert message
type HealthAlertMessage struct {
	Type           string  `json:"type"`
	User           string  `json:"user"`
	HealthFactor   float64 `json:"healthFactor"`
	Recommendation string  `json:"recommendation"`
	Timestamp      int64   `json:"timestamp"`
}

// WSHandler handles WebSocket connections
type WSHandler struct {
	clients    map[*websocket.Conn]string // map connection to wallet address
	clientsMux sync.RWMutex
	broadcast  chan HealthAlertMessage
}

// NewWSHandler creates a new WebSocket handler
func NewWSHandler() *WSHandler {
	h := &WSHandler{
		clients:   make(map[*websocket.Conn]string),
		broadcast: make(chan HealthAlertMessage, 100),
	}
	go h.handleBroadcast()
	return h
}

// handleBroadcast handles broadcasting messages to connected clients
func (h *WSHandler) handleBroadcast() {
	for msg := range h.broadcast {
		h.clientsMux.RLock()
		for conn, wallet := range h.clients {
			// Only send to the specific user or to all if user is empty
			if msg.User == "" || msg.User == wallet {
				if err := conn.WriteJSON(msg); err != nil {
					log.Printf("Error writing to WebSocket: %v", err)
				}
			}
		}
		h.clientsMux.RUnlock()
	}
}

// HealthAlert handles WS /api/v1/ws/health-alert
func (h *WSHandler) HealthAlert(c *gin.Context) {
	// Get wallet address from query param or JWT
	wallet := c.Query("wallet")
	if wallet == "" {
		walletAddr, exists := c.Get("walletAddress")
		if exists {
			wallet = walletAddr.(string)
		}
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return
	}

	// Register client
	h.clientsMux.Lock()
	h.clients[conn] = wallet
	h.clientsMux.Unlock()

	log.Printf("WebSocket client connected: %s", wallet)

	// Send welcome message
	welcomeMsg := HealthAlertMessage{
		Type:      "CONNECTED",
		User:      wallet,
		Timestamp: time.Now().Unix(),
	}
	conn.WriteJSON(welcomeMsg)

	// Handle incoming messages (keep connection alive)
	defer func() {
		h.clientsMux.Lock()
		delete(h.clients, conn)
		h.clientsMux.Unlock()
		conn.Close()
		log.Printf("WebSocket client disconnected: %s", wallet)
	}()

	for {
		// Read message (for ping/pong handling)
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
	}
}

// SendHealthAlert sends a health factor alert to a specific user
func (h *WSHandler) SendHealthAlert(wallet string, healthFactor float64, recommendation string) {
	msg := HealthAlertMessage{
		Type:           "HEALTH_ALERT",
		User:           wallet,
		HealthFactor:   healthFactor,
		Recommendation: recommendation,
		Timestamp:      time.Now().Unix(),
	}
	h.broadcast <- msg
}

// BroadcastAll sends a message to all connected clients
func (h *WSHandler) BroadcastAll(msg HealthAlertMessage) {
	h.broadcast <- msg
}

// GetConnectedClients returns the number of connected clients
func (h *WSHandler) GetConnectedClients() int {
	h.clientsMux.RLock()
	defer h.clientsMux.RUnlock()
	return len(h.clients)
}
