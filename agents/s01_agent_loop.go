// s01_agent_loop.go - Core agent loop: the simplest possible harness.
package agents

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func init() {
	_ = os.Chdir(WorkDir)
}

// RunBash executes a shell command with safety checks.
func RunBash(command string) string {
	if IsDangerous(command) {
		return "Error: Dangerous command blocked"
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(err.Error(), "timeout") {
			return "Error: Timeout (120s)"
		}
		return fmt.Sprintf("Error: %v", err)
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return "(no output)"
	}
	if len(result) > MaxOutputLen {
		return result[:MaxOutputLen]
	}
	return result
}

// AgentLoop runs the LLM + tool execution loop.
func AgentLoop(messages []Message, systemPrompt string, tools []ToolDefinition) ([]Message, error) {
	for round := 0; round < 100; round++ {
		resp, err := SendMessage(systemPrompt, messages, tools)
		if err != nil {
			return messages, err
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		if resp.StopReason != "tool_use" {
			return messages, nil
		}

		results := ExecuteTools(resp.Content, tools)
		messages = append(messages, Message{Role: "user", Content: results})
	}
	return messages, fmt.Errorf("max iterations reached")
}

// ExecuteTools dispatches tool calls and returns results.
func ExecuteTools(blocks []interface{}, tools []ToolDefinition) []interface{} {
	var results []interface{}
	for _, b := range blocks {
		toolUse, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		if toolUse["type"] != "tool_use" {
			continue
		}
		name, _ := toolUse["name"].(string)
		input, _ := toolUse["input"].(map[string]interface{})
		id, _ := toolUse["id"].(string)

		output := fmt.Sprintf("Unknown tool: %s", name)
		switch name {
		case "bash":
			if cmd, ok := input["command"].(string); ok {
				output = RunBash(cmd)
			}
		}

		results = append(results, map[string]interface{}{
			"type":          "tool_result",
			"tool_use_id":   id,
			"content":       output,
		})
	}
	return results
}

// ToolSchemas returns the minimal tool definitions for s01.
func BashToolSchema() ToolDefinition {
	return ToolDefinition{
		Name:        "bash",
		Description: "Run a shell command.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string"},
			},
			"required": []string{"command"},
		},
	}
}

// REPL runs an interactive loop for s01.
func RunREPL() {
	systemPrompt := fmt.Sprintf("You are a coding agent at %s. Use tools to solve tasks.", WorkDir)
	var messages []Message
	tools := []ToolDefinition{BashToolSchema()}

	fmt.Println("s01 agent (type 'q' to exit)")
	for {
		fmt.Print("\033[36ms01 >> \033[0m")
		var input string
		fmt.Scanln(&input)
		if strings.TrimSpace(strings.ToLower(input)) == "q" {
			break
		}
		messages = append(messages, Message{Role: "user", Content: input})
		messages, _ = AgentLoop(messages, systemPrompt, tools)
		PrintLastResponse(messages)
	}
}

// PrintLastResponse prints the assistant's text response.
func PrintLastResponse(messages []Message) {
	if len(messages) == 0 {
		return
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" {
		return
	}
	content, ok := last.Content.([]interface{})
	if !ok {
		if s, ok := last.Content.(string); ok {
			fmt.Println(s)
		}
		return
	}
	for _, block := range content {
		if tb, ok := block.(map[string]interface{}); ok {
			if tb["type"] == "text" {
				if text, ok := tb["text"].(string); ok {
					fmt.Println(text)
				}
			}
		}
	}
}

func _s01_main() {
	// Compatibility shim: load .env if available
	LoadEnv()

	system := fmt.Sprintf("You are a coding agent at %s. Use tools to solve tasks.", WorkDir)
	tools := []ToolDefinition{BashToolSchema()}

	var messages []Message
	fmt.Println("s01 agent (type 'q' to exit)")
	for {
		fmt.Print("\033[36ms01 >> \033[0m")
		var input string
		fmt.Scanln(&input)
		if strings.TrimSpace(strings.ToLower(input)) == "q" {
			break
		}
		messages = append(messages, Message{Role: "user", Content: input})

		resp, err := SendMessage(system, messages, tools)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			break
		}
		messages = append(messages, Message{Role: "assistant", Content: resp.Content})

		if resp.StopReason != "tool_use" {
			PrintLastResponse(messages)
			continue
		}

		// Tool execution
		blocks := resp.Content
		var toolResults []interface{}
		for _, b := range blocks {
			toolUse, ok := b.(map[string]interface{})
			if !ok || toolUse["type"] != "tool_use" {
				continue
			}
			name, _ := toolUse["name"].(string)
			inputMap, _ := toolUse["input"].(map[string]interface{})
			id, _ := toolUse["id"].(string)

			var output string
			switch name {
			case "bash":
				if cmd, ok := inputMap["command"].(string); ok {
					output = RunBash(cmd)
				}
			default:
				output = fmt.Sprintf("Unknown tool: %s", name)
			}
			fmt.Printf("> %s\n", name)
			if len(output) > 200 {
				fmt.Println(output[:200] + "...")
			} else {
				fmt.Println(output)
			}
			toolResults = append(toolResults, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": id,
				"content":     Truncate(output, 50000),
			})
		}
		messages = append(messages, Message{Role: "user", Content: toolResults})

		// Continue loop
		resp, err = SendMessage(system, messages, tools)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			break
		}
		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		PrintLastResponse(messages)
	}
}

func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
