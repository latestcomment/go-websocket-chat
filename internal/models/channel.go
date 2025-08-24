package models

import (
	"sync"

	"github.com/google/uuid"
)

type Channel struct {
	ChannelId    uuid.UUID
	Name         string
	Password     string
	Clients      map[uuid.UUID]*Client
	Messages     []Message
	LastSender   string // Track last user who sent a message for turn-based chat
	ClientCount  int    // Track number of clients in the channel
	Mu           sync.Mutex
}

type ChannelManager struct {
	Channels map[string]*Channel
	Mu       sync.Mutex
}
