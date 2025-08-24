package models

import "time"

type Message struct {
	SenderType string    `json:"senderType"` // "user" or "system"
	SenderName string    `json:"sender"`     // username or "system"
	Text       string    `json:"text"`
	Timestamp  time.Time `json:"timestamp"`
}
