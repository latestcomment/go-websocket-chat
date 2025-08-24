package services

import (
	"fmt"
	"time"

	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
	"github.com/latestcomment/go-websocket-chat/internal/models"
)

type ChannelService struct {
	Manager *models.ChannelManager
}

func NewChannelService(manager *models.ChannelManager) *ChannelService {
	return &ChannelService{Manager: manager}
}

func (s *ChannelService) CreateChannel(name string, password string) *models.Channel {
	// Create a copy of the password string to avoid reference issues
	passwordCopy := string([]byte(password))
	
	ch := &models.Channel{
		ChannelId:   uuid.New(),
		Name:        name,
		Password:    passwordCopy,
		Clients:     make(map[uuid.UUID]*models.Client),
		Messages:    []models.Message{},
		ClientCount: 0,
	}
	
	s.Manager.Mu.Lock()
	s.Manager.Channels[name] = ch
	s.Manager.Mu.Unlock()

	fmt.Printf("Channel created: %s (%s)\n", name, ch.ChannelId)
	return ch
}

func (s *ChannelService) GetChannel(name string) *models.Channel {
	s.Manager.Mu.Lock()
	defer s.Manager.Mu.Unlock()
	return s.Manager.Channels[name]
}

func (s *ChannelService) AddClient(ch *models.Channel, c *models.Client) {
	ch.Mu.Lock()
	ch.Clients[c.Id] = c
	ch.ClientCount++
	ch.Mu.Unlock()

	joinMsg := models.Message{
		SenderType: "system",
		SenderName: "system",
		Text:       fmt.Sprintf("%s joined the chat", c.Name),
		Timestamp:  time.Now(),
	}
	s.BroadcastMessage(ch, joinMsg)

	// When 2 people have joined, ask if they're ready
	if ch.ClientCount == 2 {
		readyMsg := models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🤔 Two participants have joined! Are you ready to engage in the debate? Click 'Engage' when you're ready to begin.",
			Timestamp:  time.Now(),
		}
		s.BroadcastMessage(ch, readyMsg)
	}
}

func (s *ChannelService) RemoveClient(ch *models.Channel, c *models.Client) {
	ch.Mu.Lock()
	delete(ch.Clients, c.Id)
	ch.ClientCount--
	ch.Mu.Unlock()

	leaveMsg := models.Message{
		SenderType: "system",
		SenderName: "system",
		Text:       fmt.Sprintf("%s left the chat", c.Name),
		Timestamp:  time.Now(),
	}
	s.BroadcastMessage(ch, leaveMsg)
}

func (s *ChannelService) BroadcastMessage(ch *models.Channel, msg models.Message) {
	ch.Mu.Lock()
	ch.Messages = append(ch.Messages, msg)
	// Update LastSender only for user messages (not system messages)
	if msg.SenderType == "user" {
		ch.LastSender = msg.SenderName
	}
	ch.Mu.Unlock()

	for _, client := range ch.Clients {
		if client.Conn != nil {
			client.Conn.WriteJSON(msg)
		}
	}
	fmt.Printf("[%s] %s: %s\n", ch.Name, msg.SenderName, msg.Text)
}

func (s *ChannelService) LoopMessages(ch *models.Channel, c *websocket.Conn, client *models.Client) {
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			break
		}
		
		messageText := string(data)
		
		// Handle special engage command
		if messageText == "__ENGAGE__" && client.CanSend {
			s.HandleClientEngage(ch, client)
			continue
		}
		
		if !client.CanSend {
			// Notify the client they are read-only
			c.WriteJSON(models.Message{
				SenderType: "system",
				SenderName: "system",
				Text:       "You are read-only in this room",
				Timestamp:  time.Now(),
			})
			continue
		}

		// Check if this user sent the last message (turn-based rule)
		ch.Mu.Lock()
		if ch.LastSender == client.Name {
			ch.Mu.Unlock()
			// Notify the client they must wait for their turn
			c.WriteJSON(models.Message{
				SenderType: "system",
				SenderName: "system",
				Text:       "Please wait for another user to send a message before sending again",
				Timestamp:  time.Now(),
			})
			continue
		}
		ch.Mu.Unlock()
		msg := models.Message{
			SenderType: "user",
			SenderName: client.Name,
			Text:       messageText,
			Timestamp:  time.Now(),
		}
		s.BroadcastMessage(ch, msg)
	}
}

func (s *ChannelService) SystemResponse(ch *models.Channel, status string) {
	var msg models.Message
	if status == "welcome" {
		msg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "Welcome to the channel!",
			Timestamp:  time.Now(),
		}
	} else if status == "moderation" {
		msg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "Please adhere to the community guidelines.",
			Timestamp:  time.Now(),
		}
	}
	s.BroadcastMessage(ch, msg)
}

// HandleClientEngage marks a client as ready and checks if debate can start
func (s *ChannelService) HandleClientEngage(ch *models.Channel, client *models.Client) {
	ch.Mu.Lock()
	client.Ready = true
	
	// Count how many clients are ready
	readyCount := 0
	for _, c := range ch.Clients {
		if c.Ready && c.CanSend { // Only count clients who can send (joined with password)
			readyCount++
		}
	}
	ch.Mu.Unlock()

	// Announce that this client is ready
	readyMsg := models.Message{
		SenderType: "system",
		SenderName: "system",
		Text:       fmt.Sprintf("✅ %s is ready to engage! (%d/2 participants ready)", client.Name, readyCount),
		Timestamp:  time.Now(),
	}
	s.BroadcastMessage(ch, readyMsg)

	// If both participants are ready, start the debate
	if readyCount >= 2 {
		battleStartMsg := models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🎯 The debate battle begins! Two participants are now ready to engage. Let the discussion commence!",
			Timestamp:  time.Now(),
		}
		s.BroadcastMessage(ch, battleStartMsg)
	}
}
