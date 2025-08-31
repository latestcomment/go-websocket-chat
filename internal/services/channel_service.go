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

func (s *ChannelService) CreateChannel(name string, inputPassword string) *models.Channel {

	inputPswd := string([]byte(inputPassword))
	phase := models.Phase{
		Id:       0,
		Name:     "Phase 0",
		Duration: 0, // No time limit for lobby phase
	}

	ch := &models.Channel{
		ChannelId:             uuid.New(),
		Name:                  name,
		Password:              inputPswd,
		Clients:               make(map[uuid.UUID]*models.Client),
		Messages:              []models.Message{},
		ClientCount:           0,
		Phase:                 phase,
		PhaseParticipants:     make(map[string]map[string]bool),
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
		
		// Check if we should progress to next phase
		s.checkPhaseProgression(ch, client)
	}
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
	if readyCount == 2 {
		battleStartMsg := models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🎯 The debate battle begins! Two participants are now ready to engage. Let the discussion commence!",
			Timestamp:  time.Now(),
		}
		ch.Mu.Lock()
		ch.Phase = models.Phase{
			Id:        1,
			Name:      "Phase 1",
			StartTime: time.Now(),
			Duration:  3 * time.Minute,
		}
		ch.Mu.Unlock()
		s.BroadcastMessage(ch, battleStartMsg)
		
		// Announce Phase 1
		phase1Msg := s.getPhaseMessage(1)
		s.BroadcastMessage(ch, phase1Msg)
		
		// Initialize phase participant tracking
		ch.PhaseParticipants = make(map[string]map[string]bool)
	}
}

// checkPhaseProgression checks if both participants have contributed and advances phase
func (s *ChannelService) checkPhaseProgression(ch *models.Channel, client *models.Client) {
	ch.Mu.Lock()

	currentPhase := ch.Phase.Id
	if currentPhase == 0 || currentPhase >= 6 {
		ch.Mu.Unlock()
		return // Not in active debate phases
	}

	// Count how many active participants we have
	activeParticipants := 0
	for _, c := range ch.Clients {
		if c.CanSend {
			activeParticipants++
		}
	}

	// Track that this participant has contributed to current phase
	phaseKey := fmt.Sprintf("phase_%d", currentPhase)
	if ch.PhaseParticipants[phaseKey] == nil {
		ch.PhaseParticipants[phaseKey] = make(map[string]bool)
	}
	
	// Mark this participant as having contributed
	ch.PhaseParticipants[phaseKey][client.Name] = true

	// Check if all participants have contributed
	if len(ch.PhaseParticipants[phaseKey]) >= activeParticipants {
		// All participants have contributed, move to next phase
		// Unlock before calling progressToNextPhase to avoid deadlock
		ch.Mu.Unlock()
		s.progressToNextPhase(ch)
		return
	}
	
	ch.Mu.Unlock()
}

// progressToNextPhase advances the debate to the next phase
func (s *ChannelService) progressToNextPhase(ch *models.Channel) {
	ch.Mu.Lock()
	
	nextPhaseId := ch.Phase.Id + 1
	
	if nextPhaseId > 5 {
		// Debate concluded
		ch.Mu.Unlock()
		endMsg := models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🏁 The debate has concluded. Thank you for participating!",
			Timestamp:  time.Now(),
		}
		s.BroadcastMessage(ch, endMsg)
		return
	}

	var nextDuration time.Duration
	switch nextPhaseId {
	case 2, 3, 4:
		nextDuration = 2 * time.Minute
	case 5:
		nextDuration = 3 * time.Minute
	default:
		nextDuration = 0
	}

	ch.Phase = models.Phase{
		Id:        nextPhaseId,
		Name:      fmt.Sprintf("Phase %d", nextPhaseId),
		StartTime: time.Now(),
		Duration:  nextDuration,
	}

	// Clear participant tracking for new phase
	phaseKey := fmt.Sprintf("phase_%d", nextPhaseId)
	ch.PhaseParticipants[phaseKey] = make(map[string]bool)

	// Announce new phase
	phaseMsg := s.getPhaseMessage(nextPhaseId)
	ch.Mu.Unlock()
	s.BroadcastMessage(ch, phaseMsg)
}

// getPhaseMessage returns the appropriate message for a phase
func (s *ChannelService) getPhaseMessage(phaseId int) models.Message {
	var phaseMsg models.Message

	switch phaseId {
	case 1:
		phaseMsg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "📢 Phase 1: Opening Statements - Each participant presents their initial arguments. You have 3 minutes each.",
			Timestamp:  time.Now(),
		}
	case 2:
		phaseMsg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🔄 Phase 2: Rebuttals - Participants respond to each other's opening statements. You have 2 minutes each.",
			Timestamp:  time.Now(),
		}
	case 3:
		phaseMsg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "❓ Phase 3: Questions - Participants ask each other questions. You have 2 minutes per question.",
			Timestamp:  time.Now(),
		}
	case 4:
		phaseMsg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "💬 Phase 4: Answering Questions - Participants answer the questions posed. You have 2 minutes per answer.",
			Timestamp:  time.Now(),
		}
	case 5:
		phaseMsg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🎯 Phase 5: Closing Statements - Each participant summarizes their key points. You have 3 minutes each.",
			Timestamp:  time.Now(),
		}
	default:
		phaseMsg = models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "🏁 The debate has concluded. Thank you for participating!",
			Timestamp:  time.Now(),
		}
	}
	return phaseMsg
}