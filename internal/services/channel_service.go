package services

import (
	"fmt"
	"strings"
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
		PendingMessages:       []models.Message{},
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


		msg := models.Message{
			SenderType: "user",
			SenderName: client.Name,
			Text:       messageText,
			Timestamp:  time.Now(),
		}
		
		// During active debate phases (1-5), store messages as pending
		ch.Mu.Lock()
		currentPhase := ch.Phase.Id
		ch.Mu.Unlock()
		
		if currentPhase >= 1 && currentPhase <= 5 {
			s.handlePhaseMessage(ch, msg, client)
		} else {
			// In phase 0 (lobby), broadcast immediately
			s.BroadcastMessage(ch, msg)
		}
	}
}

// handlePhaseMessage handles messages during active debate phases
func (s *ChannelService) handlePhaseMessage(ch *models.Channel, msg models.Message, client *models.Client) {
	ch.Mu.Lock()
	
	currentPhase := ch.Phase.Id
	phaseKey := fmt.Sprintf("phase_%d", currentPhase)
	
	// Initialize phase participants if needed
	if ch.PhaseParticipants[phaseKey] == nil {
		ch.PhaseParticipants[phaseKey] = make(map[string]bool)
	}
	
	// Check if this participant has already submitted for this phase
	if ch.PhaseParticipants[phaseKey][client.Name] {
		ch.Mu.Unlock()
		// Already submitted, send notification
		errorMsg := models.Message{
			SenderType: "system",
			SenderName: "system",
			Text:       "⏳ You have already submitted your response for this phase. Please wait for other participants.",
			Timestamp:  time.Now(),
		}
		// Send only to this client
		if client.Conn != nil {
			client.Conn.WriteJSON(errorMsg)
		}
		return
	}
	
	// Add message to pending and mark participant as contributed
	ch.PendingMessages = append(ch.PendingMessages, msg)
	ch.PhaseParticipants[phaseKey][client.Name] = true
	
	// Send confirmation to this client only (not broadcast)
	confirmMsg := models.Message{
		SenderType: "system", 
		SenderName: "system",
		Text:       "✅ Your response has been submitted. Waiting for other participants...",
		Timestamp:  time.Now(),
	}
	if client.Conn != nil {
		client.Conn.WriteJSON(confirmMsg)
	}
	
	// Count active participants
	activeParticipants := 0
	for _, c := range ch.Clients {
		if c.CanSend {
			activeParticipants++
		}
	}
	
	// Check if all participants have submitted
	if len(ch.PhaseParticipants[phaseKey]) >= activeParticipants {
		// Release all pending messages simultaneously
		pendingMsgs := make([]models.Message, len(ch.PendingMessages))
		copy(pendingMsgs, ch.PendingMessages)
		ch.PendingMessages = []models.Message{}
		
		// Unlock before broadcasting and phase completion
		ch.Mu.Unlock()
		
		// Broadcast all pending messages
		for _, msg := range pendingMsgs {
			s.BroadcastMessage(ch, msg)
		}
		
		// Notify that AI analysis is starting
		aiStartMsg := models.Message{
			SenderType: "system",
			SenderName: "system", 
			Text:       "🤖 AI Moderator is analyzing the responses...",
			Timestamp:  time.Now(),
		}
		s.BroadcastMessage(ch, aiStartMsg)
		
		// Handle phase completion
		s.handlePhaseCompletion(ch, currentPhase)
		return
	}
	
	ch.Mu.Unlock()
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
		ch.PendingMessages = []models.Message{}
	}
}


// handlePhaseCompletion processes AI analysis when a phase is completed
func (s *ChannelService) handlePhaseCompletion(ch *models.Channel, completedPhase int) {
	// Collect messages from the completed phase
	phaseMessages := s.getPhaseMessages(ch, completedPhase)
	
	if len(phaseMessages) > 0 {
		// Create context for AI analysis
		context := s.createPhaseContext(completedPhase, phaseMessages)
		
		// Send AI request with phase-specific prompt
		aiResponse, err := s.sendPhaseSpecificAIRequest(completedPhase, context)
		if err != nil {
			fmt.Printf("Error getting AI analysis: %v\n", err)
			aiResponse = "Unable to provide analysis at this time."
		}
		
		// Broadcast AI analysis
		aiMessage := models.Message{
			SenderType: "ai",
			SenderName: "AI Moderator",
			Text:       fmt.Sprintf("📊 **Phase %d Analysis**: %s", completedPhase, aiResponse),
			Timestamp:  time.Now(),
		}
		s.BroadcastMessage(ch, aiMessage)
	}
	
	// Progress to next phase after AI analysis
	s.progressToNextPhase(ch)
}

// getPhaseMessages collects user messages from the specified phase
func (s *ChannelService) getPhaseMessages(ch *models.Channel, phaseId int) []models.Message {
	ch.Mu.Lock()
	defer ch.Mu.Unlock()
	
	var phaseMessages []models.Message
	phaseKey := fmt.Sprintf("phase_%d", phaseId)
	
	// Get participants who contributed in this phase
	participants := ch.PhaseParticipants[phaseKey]
	
	// Collect the most recent messages from each participant in this phase
	for _, msg := range ch.Messages {
		if msg.SenderType == "user" {
			if _, participated := participants[msg.SenderName]; participated {
				phaseMessages = append(phaseMessages, msg)
			}
		}
	}
	
	return phaseMessages
}

// createPhaseContext creates context string for AI analysis
func (s *ChannelService) createPhaseContext(phaseId int, messages []models.Message) string {
	var phaseName string
	switch phaseId {
	case 1:
		phaseName = "Opening Statements"
	case 2:
		phaseName = "Rebuttals"
	case 3:
		phaseName = "Questions"
	case 4:
		phaseName = "Answers"
	case 5:
		phaseName = "Closing Statements"
	default:
		phaseName = "Discussion"
	}
	
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Phase %d - %s:\n\n", phaseId, phaseName))
	
	for _, msg := range messages {
		builder.WriteString(fmt.Sprintf("%s: %s\n", msg.SenderName, msg.Text))
	}
	
	return builder.String()
}

// parseJudgeResponse parses the structured AI judge response into JudgeReport
func (s *ChannelService) parseJudgeResponse(response string) *models.JudgeReport {
	judgeReport := &models.JudgeReport{}
	
	// Split response by #### headers
	sections := strings.Split(response, "####")
	
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		
		// Split header from content
		lines := strings.Split(section, "\n")
		if len(lines) < 2 {
			continue
		}
		
		header := strings.TrimSpace(lines[0])
		content := strings.TrimSpace(strings.Join(lines[1:], "\n"))
		
		// Match header to field
		switch {
		case strings.Contains(strings.ToLower(header), "winner declaration"):
			judgeReport.WinnerDeclaration = content
		case strings.Contains(strings.ToLower(header), "argument analysis"):
			judgeReport.ArgumentAnalysis = content
		case strings.Contains(strings.ToLower(header), "debate performance"):
			judgeReport.DebatePerformance = content
		case strings.Contains(strings.ToLower(header), "evidence") && strings.Contains(strings.ToLower(header), "logic"):
			judgeReport.EvidenceLogic = content
		case strings.Contains(strings.ToLower(header), "persuasiveness"):
			judgeReport.Persuasiveness = content
		case strings.Contains(strings.ToLower(header), "key turning points"):
			judgeReport.KeyTurningPoints = content
		case strings.Contains(strings.ToLower(header), "final score"):
			judgeReport.FinalScore = content
		}
	}
	
	return judgeReport
}

// sendPhaseSpecificAIRequest sends AI request with phase-appropriate prompt
func (s *ChannelService) sendPhaseSpecificAIRequest(phaseId int, context string) (string, error) {
	var prompt string
	
	switch phaseId {
	case 1:
		prompt = `You are analyzing the opening statements phase of a debate. Please:
1. Summarize the main arguments presented by each participant
2. Identify the key positions and claims made
3. Note the strength and clarity of each opening statement
4. Point out any logical fallacies or weak arguments
5. Assess whether the statements stay on topic
Keep your analysis balanced, constructive, and under 200 words.`

	case 2:
		prompt = `You are analyzing the rebuttals phase of a debate. Please:
1. Evaluate how well each participant addressed their opponent's arguments
2. Identify effective counterarguments and refutations
3. Note any new evidence or points introduced
4. Point out missed opportunities to address key opposing arguments
5. Assess the logical flow and persuasiveness of the rebuttals
Keep your analysis balanced, constructive, and under 200 words.`

	case 3:
		prompt = `You are analyzing the questions phase of a debate. Please:
1. Evaluate the quality and relevance of questions asked
2. Assess whether questions effectively challenge key arguments
3. Note if questions are fair and constructive or leading/hostile
4. Identify strategic questioning that exposes weaknesses
5. Point out any questions that are off-topic or inappropriate
Keep your analysis balanced, constructive, and under 200 words.`

	case 4:
		prompt = `You are analyzing the answers phase of a debate. Please:
1. Evaluate how well each participant answered the questions posed
2. Note any evasive or incomplete answers
3. Assess the honesty and directness of responses
4. Identify strong, evidence-based answers
5. Point out when answers introduce new relevant information
Keep your analysis balanced, constructive, and under 200 words.`

	case 5:
		prompt = `You are analyzing the closing statements phase of a debate. Please:
1. Evaluate how well each participant summarized their key arguments
2. Assess the persuasiveness and emotional impact of closing statements
3. Note effective use of evidence and logic in conclusions
4. Identify the strongest final points made by each side
5. Provide an overall assessment of which arguments were most compelling
Keep your analysis balanced, constructive, and under 200 words.`

	default:
		prompt = `You are a moderator in this discussion. Please provide a balanced summary of the main points discussed, highlighting different perspectives and any consensus reached. Keep it concise and neutral. And point out any potential out of topic or inappropriate comments.`
	}

	return SendAIRequestWithCustomPrompt(prompt, context)
}

// provideFinalAIJudgment provides final AI verdict after all phases
func (s *ChannelService) provideFinalAIJudgment(ch *models.Channel) {
	// Notify that AI Judge is generating verdict
	judgeStartMsg := models.Message{
		SenderType: "system",
		SenderName: "system",
		Text:       "⚖️ AI Judge is evaluating the complete debate and preparing the final verdict...",
		Timestamp:  time.Now(),
	}
	s.BroadcastMessage(ch, judgeStartMsg)
	
	// Collect all debate messages for comprehensive analysis
	allDebateMessages := s.getAllDebateMessages(ch)
	
	if len(allDebateMessages) > 0 {
		// Create comprehensive context for final judgment
		context := s.createFinalJudgmentContext(allDebateMessages)
		
		// Get AI judgment
		judgmentPrompt := `You are an impartial AI judge evaluating this complete debate. Please provide your verdict using EXACTLY this structure with these section headers:

#### Winner Declaration
[Clearly state which participant won and provide a brief justification]

#### Argument Analysis  
[Evaluate the strongest and weakest arguments from each side]

#### Debate Performance
[Assess how well each participant engaged in the structured debate format]

#### Evidence & Logic
[Comment on the quality of evidence, reasoning, and logical consistency]

#### Persuasiveness
[Determine which viewpoint was most compelling and convincing]

#### Key Turning Points
[Identify critical moments that influenced the debate outcome]

#### Final Score
[Provide a score out of 10 for each participant with brief justification]

Be decisive in your judgment while explaining your reasoning. Use the exact section headers above with #### formatting.`

		aiJudgment, err := SendAIRequestWithCustomPrompt(judgmentPrompt, context)
		if err != nil {
			fmt.Printf("Error getting AI judgment: %v\n", err)
			aiJudgment = "Unable to provide final judgment at this time."
		}

		// Parse structured judgment
		judgeData := s.parseJudgeResponse(aiJudgment)
		
		// Broadcast final judgment with structured data
		judgmentMessage := models.Message{
			SenderType: "judge",
			SenderName: "AI Judge",
			Text:       fmt.Sprintf("⚖️ **FINAL VERDICT** ⚖️\n\n%s", aiJudgment),
			Timestamp:  time.Now(),
			JudgeData:  judgeData,
		}
		s.BroadcastMessage(ch, judgmentMessage)
	}

	// Send conclusion message
	endMsg := models.Message{
		SenderType: "system",
		SenderName: "system",
		Text:       "🏁 The debate has concluded. Thank you for participating!",
		Timestamp:  time.Now(),
	}
	s.BroadcastMessage(ch, endMsg)
}

// getAllDebateMessages collects all user messages from the debate
func (s *ChannelService) getAllDebateMessages(ch *models.Channel) []models.Message {
	ch.Mu.Lock()
	defer ch.Mu.Unlock()
	
	var debateMessages []models.Message
	
	// Collect all user messages (excluding system and ai messages)
	for _, msg := range ch.Messages {
		if msg.SenderType == "user" {
			debateMessages = append(debateMessages, msg)
		}
	}
	
	return debateMessages
}

// createFinalJudgmentContext creates comprehensive context for final AI judgment
func (s *ChannelService) createFinalJudgmentContext(messages []models.Message) string {
	var builder strings.Builder
	builder.WriteString("Complete Debate Transcript:\n")
	builder.WriteString("=======================\n\n")
	
	// Group messages by phase for better context
	phaseMessages := make(map[int][]models.Message)
	currentPhase := 1
	
	for _, msg := range messages {
		phaseMessages[currentPhase] = append(phaseMessages[currentPhase], msg)
		
		// Simple heuristic: assume roughly equal distribution across phases
		// In a real implementation, you might track phase transitions more precisely
		if len(phaseMessages[currentPhase]) >= 2 && currentPhase < 5 {
			currentPhase++
		}
	}
	
	// Format by phases
	phaseNames := map[int]string{
		1: "Opening Statements",
		2: "Rebuttals", 
		3: "Questions",
		4: "Answers",
		5: "Closing Statements",
	}
	
	for phase := 1; phase <= 5; phase++ {
		if msgs, exists := phaseMessages[phase]; exists && len(msgs) > 0 {
			builder.WriteString(fmt.Sprintf("## Phase %d - %s:\n", phase, phaseNames[phase]))
			for _, msg := range msgs {
				builder.WriteString(fmt.Sprintf("**%s**: %s\n\n", msg.SenderName, msg.Text))
			}
			builder.WriteString("\n")
		}
	}
	
	return builder.String()
}

// progressToNextPhase advances the debate to the next phase
func (s *ChannelService) progressToNextPhase(ch *models.Channel) {
	ch.Mu.Lock()
	
	nextPhaseId := ch.Phase.Id + 1
	
	if nextPhaseId > 5 {
		// Debate concluded, get final AI judgment
		ch.Mu.Unlock()
		s.provideFinalAIJudgment(ch)
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