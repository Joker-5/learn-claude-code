// s06_context_compact.go - Three-layer compression pipeline for infinite sessions.
package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	TokenThreshold  = 50000
	KeepRecent     = 3
	TranscriptDir   = ".transcripts"
)

var PreserveResultTools = map[string]bool{
	"read_file": true,
}

// MicroCompact (Layer 1) replaces old tool results with placeholders, keeping the last KEEP_RECENT.
func MicroCompact(messages []Message) {
	toolResults := make([][2]int, 0) // [msg_index, part_index]
	for msgIdx, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		content, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for partIdx, part := range content {
			partMap, ok := part.(map[string]interface{})
			if !ok || partMap["type"] != "tool_result" {
				continue
			}
			toolResults = append(toolResults, [2]int{msgIdx, partIdx})
		}
	}
	if len(toolResults) <= KeepRecent {
		return
	}

	// Build tool_name_map from prior assistant messages
	toolNameMap := make(map[string]string)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		blocks, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for _, b := range blocks {
			bMap, ok := b.(map[string]interface{})
			if !ok || bMap["type"] != "tool_use" {
				continue
			}
			if id, ok := bMap["id"].(string); ok {
				if name, ok := bMap["name"].(string); ok {
					toolNameMap[id] = name
				}
			}
		}
	}

	// Clear old results (keep last KEEP_RECENT)
	toClear := toolResults[:len(toolResults)-KeepRecent]
	for _, idx := range toClear {
		msgIdx, partIdx := idx[0], idx[1]
		content := messages[msgIdx].Content.([]interface{})
		part := content[partIdx].(map[string]interface{})

		partContent, ok := part["content"].(string)
		if !ok || len(partContent) <= 100 {
			continue
		}
		toolID, _ := part["tool_use_id"].(string)
		toolName := toolNameMap[toolID]
		if toolName == "" {
			toolName = "unknown"
		}
		if PreserveResultTools[toolName] {
			continue
		}
		part["content"] = "[Previous: used " + toolName + "]"
	}
}

// AutoCompact (Layer 2) saves transcript, summarizes with LLM, and replaces messages.
func AutoCompact(messages []Message, systemPrompt string) ([]Message, error) {
	// Save transcript
	transcriptDir := filepath.Join(WorkDir, TranscriptDir)
	if err := os.MkdirAll(transcriptDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create transcript dir: %w", err)
	}
	transcriptPath := filepath.Join(transcriptDir, fmt.Sprintf("transcript_%d.jsonl", time.Now().Unix()))

	f, err := os.Create(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create transcript: %w", err)
	}
	for _, msg := range messages {
		// Simple JSON serialization without the full encoder
		serialized := serializeMessage(msg)
		f.WriteString(serialized + "\n")
	}
	f.Close()

	// Build conversation text for summarization
	var sb strings.Builder
	for _, msg := range messages {
		serialized := serializeMessage(msg)
		full := serialized
		if len(full) > 80000 {
			full = full[len(full)-80000:]
		}
		sb.WriteString(full)
	}

	summaryPrompt := fmt.Sprintf(`Summarize this conversation for continuity. Include:
1) What was accomplished
2) Current state
3) Key decisions made
Be concise but preserve critical details.

%s`, sb.String())

	resp, err := SendMessage("", []Message{{Role: "user", Content: summaryPrompt}}, nil)
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	summary := extractText(resp.Content)
	if summary == "" {
		summary = "No summary generated."
	}

	return []Message{{
		Role:    "user",
		Content: fmt.Sprintf("[Conversation compressed. Transcript: %s]\n\n%s", transcriptPath, summary),
	}}, nil
}

func serializeMessage(msg Message) string {
	// Simple serialization: role: content
	switch c := msg.Content.(type) {
	case string:
		return msg.Role + ": " + c
	case []interface{}:
		var parts []string
		for _, p := range c {
			if pm, ok := p.(map[string]interface{}); ok {
				switch pm["type"] {
				case "text":
					if t, ok := pm["text"].(string); ok {
						parts = append(parts, "text:"+t)
					}
				case "tool_result":
					if id, ok := pm["tool_use_id"].(string); ok {
						if cnt, ok := pm["content"].(string); ok {
							parts = append(parts, "tool_result["+id+"]:"+cnt)
						}
					}
				}
			}
		}
		return msg.Role + ": " + strings.Join(parts, " | ")
	}
	return msg.Role + ": (unknown)"
}

func extractText(blocks []interface{}) string {
	var sb strings.Builder
	for _, b := range blocks {
		if bm, ok := b.(map[string]interface{}); ok && bm["type"] == "text" {
			if t, ok := bm["text"].(string); ok {
				sb.WriteString(t)
			}
		}
	}
	return sb.String()
}

// CompressToolDefinition returns the tool schema for the compress tool.
func CompressToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "compress",
		Description: "Manually compress conversation context.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		},
	}
}

