package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

type RequestPayload struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type Message struct {
	Role    string `json:"role"` // "user" or "system"
	Content string `json:"content"`
}

type ApiResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
	}
}

func SendAIRequestWithCustomPrompt(customPrompt, context string) (string, error) {
	if err := godotenv.Load(); err != nil {
		fmt.Println("No .env file found")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY not found in environment")
	}

	modelName := os.Getenv("AI_MODEL")
	if modelName == "" {
		modelName = "deepseek/deepseek-chat-v3.1:free" // Default fallback
	}

	systemMessage := Message{
		Role:    "system",
		Content: customPrompt,
	}

	userMessage := Message{
		Role:    "user",
		Content: context,
	}

	payload := RequestPayload{
		Model:     modelName,
		Messages:  []Message{systemMessage, userMessage},
		MaxTokens: 8192,
	}

	jsonData, _ := json.Marshal(payload)

	req, _ := http.NewRequest(
		"POST",
		"https://openrouter.ai/api/v1/chat/completions",
		bytes.NewBuffer(jsonData),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var apiResponse ApiResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		fmt.Println("Error parsing JSON:", err)
	}
	if len(apiResponse.Choices) > 0 {
		return apiResponse.Choices[0].Message.Content, nil
	} else {
		fmt.Println("No response choices received")
		return fmt.Sprintf("Full response: %s", string(body)), nil
	}
}
