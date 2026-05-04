// Package agents implements an AI coding agent harness in Go.
// Each session (s01-s12) introduces one mechanism, and s_full combines all of them.
package agents

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// --- Shared types and configuration ---

var WorkDir string
var BaseURL string
var Model string
var HTTPClient *http.Client

func init() {
	WorkDir, _ = os.Getwd()
	BaseURL = os.Getenv("ANTHROPIC_BASE_URL")
	Model = os.Getenv("MODEL_ID")
	if Model == "" {
		Model = "claude-opus-4-6"
	}
	HTTPClient = &http.Client{Timeout: 120 * time.Second}
}

// --- Message types ---

type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []interface{} (content blocks)
}

type ContentBlock interface{}
type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type ToolUseBlock struct {
	Type      string                 `json:"type"`
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Input     map[string]interface{} `json:"input"`
}
type ToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// --- API types ---

type APIRequest struct {
	Model       string                 `json:"model"`
	System      string                 `json:"system,omitempty"`
	Messages    []Message              `json:"messages"`
	Tools       []ToolDefinition       `json:"tools,omitempty"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature float64               `json:"temperature,omitempty"`
}

type APIResponse struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Role       string        `json:"role"`
	Model      string        `json:"model"`
	Content    []interface{} `json:"content"`
	StopReason string        `json:"stop_reason"`
}

type APIError struct {
	Error ErrorDetail `json:"error"`
}
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- Tool types ---

type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// --- Dangerous command blocklist ---

var DangerousCommands = []string{
	"rm -rf /",
	"sudo",
	"shutdown",
	"reboot",
	"> /dev/",
}

const MaxOutputLen = 50000

func IsDangerous(command string) bool {
	for _, d := range DangerousCommands {
		if strings.Contains(command, d) {
			return true
		}
	}
	return false
}

// --- API client ---

func SendMessage(systemPrompt string, messages []Message, tools []ToolDefinition) (*APIResponse, error) {
	endpoint := BaseURL
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	if !strings.HasSuffix(endpoint, "/messages") {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/v1/messages"
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_AUTH_TOKEN")
	}

	body := APIRequest{
		Model:     Model,
		System:    systemPrompt,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: 8000,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result APIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, nil
}

// --- Token estimation ---

func EstimateTokens(messages []Message) int {
	data, _ := json.Marshal(messages)
	return len(data) / 4
}
