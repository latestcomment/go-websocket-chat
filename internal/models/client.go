package models

import (
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
)

type Client struct {
	Id      uuid.UUID       `json:"clientid"`
	Name    string          `json:"clientname"`
	Conn    *websocket.Conn `json:"-"`
	CanSend bool
	Ready   bool            `json:"ready"` // Track if client is ready to engage
}
