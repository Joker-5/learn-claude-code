// s04_subagent.go - Spawn child agents with fresh context that return summaries.
package agents

import (
	"fmt"
	"strings"
)

const SubagentSystemPrompt = `You are a subagent. Work independently, then return a concise summary of what you found or did.`

// SubagentTools returns the tool definitions available to subagents (Explore type).
func SubagentTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "bash",
			Description: "Run command.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
	}
}

// SubagentToolsGeneral returns tools for "general-purpose" subagents (can write/edit).
func SubagentToolsGeneral() []ToolDefinition {
	base := SubagentTools()
	return append(base,
		ToolDefinition{
			Name:        "write_file",
			Description: "Write file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string"},
					"content": map[string]interface{}{"type": "string"},
				},
				"required": []string{"path", "content"},
			},
		},
		ToolDefinition{
			Name:        "edit_file",
			Description: "Edit file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":     map[string]interface{}{"type": "string"},
					"old_text": map[string]interface{}{"type": "string"},
					"new_text": map[string]interface{}{"type": "string"},
				},
				"required": []string{"path", "old_text", "new_text"},
			},
		},
	)
}

// RunSubagent spawns a child agent with fresh context and returns a summary.
func RunSubagent(prompt string, agentType string, systemPrompt string) string {
	registry := NewToolRegistry()

	messages := []Message{{Role: "user", Content: prompt}}

	var tools []ToolDefinition
	if agentType == "general-purpose" {
		tools = SubagentToolsGeneral()
		registry.Register("write_file", func(args map[string]interface{}) string {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			return RunWrite(path, content)
		})
		registry.Register("edit_file", func(args map[string]interface{}) string {
			path, _ := args["path"].(string)
			oldText, _ := args["old_text"].(string)
			newText, _ := args["new_text"].(string)
			return RunEdit(path, oldText, newText)
		})
	} else {
		tools = SubagentTools()
	}

	system := SubagentSystemPrompt
	if systemPrompt != "" {
		system = systemPrompt
	}

	for i := 0; i < 30; i++ {
		resp, err := SendMessage(system, messages, tools)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		messages = append(messages, Message{Role: "assistant", Content: resp.Content})

		if resp.StopReason != "tool_use" {
			// Return text content
			if blocks := resp.Content; blocks != nil {
				var sb strings.Builder
				for _, b := range blocks {
					if tb, ok := b.(map[string]interface{}); ok && tb["type"] == "text" {
						if text, ok := tb["text"].(string); ok {
							sb.WriteString(text)
						}
					}
				}
				if sb.Len() > 0 {
					return sb.String()
				}
			}
			return "(no summary)"
		}

		results := ExecuteAllTools(resp.Content, registry)
		messages = append(messages, Message{Role: "user", Content: results})
	}

	return "(max iterations reached)"
}

// SubagentToolDefinition returns the tool schema for the "task" subagent tool.
func SubagentToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "task",
		Description: "Spawn a subagent for isolated exploration or work.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt": map[string]interface{}{"type": "string"},
				"agent_type": map[string]interface{}{
					"type": "string",
					"enum":  []interface{}{"Explore", "general-purpose"},
				},
			},
			"required": []string{"prompt"},
		},
	}
}
