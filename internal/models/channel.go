package models

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type Channel struct {
	ChannelId              uuid.UUID
	Name                   string
	Password               string
	Clients                map[uuid.UUID]*Client
	Messages               []Message
	PendingMessages        []Message                   // Messages waiting to be revealed simultaneously
	ClientCount            int
	Phase                  Phase
	PhaseParticipants      map[string]map[string]bool // Track which participants contributed in each phase
	Mu                     sync.Mutex
}

type Phase struct {
	Id        int
	Name      string
	StartTime time.Time
	Duration  time.Duration
}

type ChannelManager struct {
	Channels map[string]*Channel
	Mu       sync.Mutex
}
