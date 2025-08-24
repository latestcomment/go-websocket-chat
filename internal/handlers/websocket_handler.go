package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
	"github.com/latestcomment/go-websocket-chat/internal/models"
	"github.com/latestcomment/go-websocket-chat/internal/services"
)

type WebSocketHandler struct {
	Service *services.ChannelService
}

func NewWebSocketHandler(service *services.ChannelService) *WebSocketHandler {
	return &WebSocketHandler{Service: service}
}

func (h *WebSocketHandler) WebSocketMiddleware(c *fiber.Ctx) error {
	if websocket.IsWebSocketUpgrade(c) {
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

func (h *WebSocketHandler) HandleWebSocket(c *websocket.Conn) {
	defer func() {
		_ = c.Close()
	}()

	channelName := c.Params("channel")
	name := c.Params("name")
	password := c.Params("password")

	if name == "" {
		name = "Guest"
	}

	// Get channel
	ch := h.Service.GetChannel(channelName)
	if ch == nil {
		return // Channel doesn't exist
	}

	// Register client
	client := &models.Client{
		Id:   uuid.New(),
		Name: name,
		Conn: c,
	}
	
	// Determine client permissions
	if password != "" {
		// User is trying to join with a password - validate it
		if password == ch.Password {
			client.CanSend = true
		} else {
			// Invalid password for join attempt - disconnect
			return
		}
	} else {
		// No password provided - watch-only mode
		client.CanSend = false
	}
	
	h.Service.AddClient(ch, client)
	h.Service.LoopMessages(ch, c, client)
	h.Service.RemoveClient(ch, client)
}
