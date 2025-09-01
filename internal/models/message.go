package models

import "time"

type Message struct {
	SenderType string       `json:"senderType"` // "user", "system", "ai", "judge"
	SenderName string       `json:"sender"`     // username or "system" or "AI Moderator" or "AI Judge"
	Text       string       `json:"text"`
	Timestamp  time.Time    `json:"timestamp"`
	JudgeData  *JudgeReport `json:"judgeData,omitempty"` // Structured judge report
}

type JudgeReport struct {
	WinnerDeclaration string `json:"winnerDeclaration"`
	ArgumentAnalysis  string `json:"argumentAnalysis"`
	DebatePerformance string `json:"debatePerformance"`
	EvidenceLogic     string `json:"evidenceLogic"`
	Persuasiveness    string `json:"persuasiveness"`
	KeyTurningPoints  string `json:"keyTurningPoints"`
	FinalScore        string `json:"finalScore"`
}
