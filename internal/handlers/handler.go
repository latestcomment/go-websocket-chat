package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/latestcomment/go-websocket-chat/internal/services"
)

type Handler struct {
	ChannelManager *services.ChannelService
}

func NewHandler(cm *services.ChannelService) *Handler {
	return &Handler{ChannelManager: cm}
}

func (h *Handler) LoginPage(c *fiber.Ctx) error {
	return c.Render("index", nil)
}

func (h *Handler) ChannelPage(c *fiber.Ctx) error {
	name := c.FormValue("name")
	return c.Render("channel", fiber.Map{
		"Name":     name,
		"Channels": h.ChannelManager.Manager.Channels,
	})
}

func (h *Handler) CreateChannelPage(c *fiber.Ctx) error {
	name := c.FormValue("name")                // user's name
	channelName := c.FormValue("channel")      // new channel name
	channelPassword := c.FormValue("password") // new channel password

	if channelName == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Channel name required")
	}

	// Create channel if it doesn't exist
	ch := h.ChannelManager.GetChannel(channelName)
	if ch == nil {
		ch = h.ChannelManager.CreateChannel(channelName, channelPassword)
	}

	// Redirect back to channel list page
	return c.Render("channel", fiber.Map{
		"Name":     name,
		"Channels": h.ChannelManager.Manager.Channels,
	})
}

func (h *Handler) JoinChannel(c *fiber.Ctx) error {
	name := c.FormValue("name")
	room := c.FormValue("channel")
	password := c.FormValue("password")

	ch := h.ChannelManager.GetChannel(room)
	if ch == nil {
		return c.Render("channel", fiber.Map{
			"Name":     name,
			"Channels": h.ChannelManager.Manager.Channels,
			"Error":    "Channel not found",
		})
	}
	
	if password != ch.Password {
		return c.Render("channel", fiber.Map{
			"Name":     name,
			"Channels": h.ChannelManager.Manager.Channels,
			"Error":    "Invalid password for channel " + room,
		})
	}

	return c.Render("chat", fiber.Map{
		"Name":     name,
		"Channel":  room,
		"CanSend":  true,
		"Password": password,
	})
}

func (h *Handler) WatchChannel(c *fiber.Ctx) error {
	name := c.FormValue("name")
	room := c.FormValue("channel")
	fmt.Printf("WatchChannel called: name='%s', room='%s'\n", name, room)
	return c.Render("chat", fiber.Map{
		"Name":    name,
		"Channel": room,
		"CanSend": false,
	})
}

func (h *Handler) ChatPage(c *fiber.Ctx) error {
	name := c.FormValue("name")
	room := c.FormValue("channel")
	password := c.FormValue("password")
	return c.Render("chat", fiber.Map{
		"Name":     name,
		"Channel":  room,
		"Password": password,
	})
}
