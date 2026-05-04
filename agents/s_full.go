// s_full.go - Full reference agent: all mechanisms combined.
package agents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	LoadEnv()
}

// === Global instances ===

var (
	TODO     *TodoManager
	SKILLS   *SkillLoader
	TASK_MGR *TaskManager
	BG       *BackgroundManager
	BUS      *MessageBus
	TEAM     *TeammateManager
)

func InitGlobals() {
	workDir := WorkDir
	skillsDir := os.Getenv("SKILLS_DIR")
	if skillsDir == "" {
		skillsDir = filepath.Join(workDir, "skills")
	}

	TODO = NewTodoManager()
	SKILLS = NewSkillLoader(skillsDir)
	TASK_MGR = NewTaskManager("")
	BG = NewBackgroundManager()
	BUS = NewMessageBus("")
	TEAM = NewTeammateManager(BUS, TASK_MGR)
}

// === Tool registry with all 21 tools ===

var fullRegistry *ToolRegistry

func buildFullRegistry() *ToolRegistry {
	r := NewToolRegistry()

	r.Register("TodoWrite", func(args map[string]interface{}) string {
		items, ok := args["items"].([]interface{})
		if !ok {
			return "Error: items required"
		}
		itemsMap := make([]map[string]interface{}, len(items))
		for i, item := range items {
			if m, ok := item.(map[string]interface{}); ok {
				itemsMap[i] = m
			}
		}
		result, err := TODO.Update(itemsMap)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result
	})

	r.Register("task", func(args map[string]interface{}) string {
		prompt, _ := args["prompt"].(string)
		agentType, _ := args["agent_type"].(string)
		if agentType == "" {
			agentType = "Explore"
		}
		return RunSubagent(prompt, agentType, "")
	})

	r.Register("load_skill", func(args map[string]interface{}) string {
		name, _ := args["name"].(string)
		return SKILLS.GetContent(name)
	})

	r.Register("compress", func(args map[string]interface{}) string {
		return "Compressing..."
	})

	r.Register("background_run", func(args map[string]interface{}) string {
		command, _ := args["command"].(string)
		timeout := 120
		if t, ok := args["timeout"].(float64); ok {
			timeout = int(t)
		}
		return BG.Run(command, timeout)
	})

	r.Register("check_background", func(args map[string]interface{}) string {
		taskID := ""
		if id, ok := args["task_id"].(string); ok {
			taskID = id
		}
		return BG.Check(taskID)
	})

	r.Register("task_create", func(args map[string]interface{}) string {
		subject, _ := args["subject"].(string)
		description, _ := args["description"].(string)
		result, err := TASK_MGR.Create(subject, description)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result
	})

	r.Register("task_get", func(args map[string]interface{}) string {
		taskID, _ := args["task_id"].(int)
		return TASK_MGR.Get(taskID)
	})

	r.Register("task_update", func(args map[string]interface{}) string {
		taskID, _ := args["task_id"].(int)
		status := ""
		if s, ok := args["status"].(string); ok {
			status = s
		}
		var addBlocked, removeBlocked []int
		if ab, ok := args["add_blocked_by"].([]interface{}); ok {
			for _, v := range ab {
				if f, ok := v.(float64); ok {
					addBlocked = append(addBlocked, int(f))
				}
			}
		}
		if rb, ok := args["remove_blocked_by"].([]interface{}); ok {
			for _, v := range rb {
				if f, ok := v.(float64); ok {
					removeBlocked = append(removeBlocked, int(f))
				}
			}
		}
		result, err := TASK_MGR.Update(taskID, status, addBlocked, removeBlocked)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result
	})

	r.Register("task_list", func(args map[string]interface{}) string {
		return TASK_MGR.ListAll()
	})

	r.Register("spawn_teammate", func(args map[string]interface{}) string {
		name, _ := args["name"].(string)
		role, _ := args["role"].(string)
		prompt, _ := args["prompt"].(string)
		return TEAM.Spawn(name, role, prompt)
	})

	r.Register("list_teammates", func(args map[string]interface{}) string {
		return TEAM.ListAll()
	})

	r.Register("send_message", func(args map[string]interface{}) string {
		to, _ := args["to"].(string)
		content, _ := args["content"].(string)
		msgType := "message"
		if mt, ok := args["msg_type"].(string); ok {
			msgType = mt
		}
		return BUS.Send("lead", to, content, msgType, nil)
	})

	r.Register("read_inbox", func(args map[string]interface{}) string {
		msgs := BUS.ReadInbox("lead")
		data, _ := json.MarshalIndent(msgs, "", "  ")
		return string(data)
	})

	r.Register("broadcast", func(args map[string]interface{}) string {
		content, _ := args["content"].(string)
		return BUS.Broadcast("lead", content, TEAM.MemberNames())
	})

	r.Register("shutdown_request", func(args map[string]interface{}) string {
		teammate, _ := args["teammate"].(string)
		return HandleShutdownRequest(teammate, BUS)
	})

	r.Register("plan_approval", func(args map[string]interface{}) string {
		reqID, _ := args["request_id"].(string)
		approve, _ := args["approve"].(bool)
		feedback := ""
		if f, ok := args["feedback"].(string); ok {
			feedback = f
		}
		return HandlePlanReview(reqID, approve, feedback, BUS)
	})

	r.Register("idle", func(args map[string]interface{}) string {
		return "Lead does not idle."
	})

	r.Register("claim_task", func(args map[string]interface{}) string {
		taskID, _ := args["task_id"].(int)
		return TASK_MGR.Claim(taskID, "lead")
	})

	return r
}

// === Tool definitions ===

func FullToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		// Base tools (4)
		ToolDefinition{
			Name:        "bash",
			Description: "Run a shell command.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
		ToolDefinition{
			Name:        "read_file",
			Description: "Read file contents.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":  map[string]interface{}{"type": "string"},
					"limit": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"path"},
			},
		},
		ToolDefinition{
			Name:        "write_file",
			Description: "Write content to file.",
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
			Description: "Replace exact text in file.",
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
		// Todo tool (1)
		TodoToolDefinition(),
		// Subagent (1)
		SubagentToolDefinition(),
		// Skills (1)
		SkillToolDefinition(),
		// Compression (1)
		CompressToolDefinition(),
		// Background tasks (2)
		BGToolDefinitions()[0],
		BGToolDefinitions()[1],
		// Tasks (4)
		TaskToolDefinitions()[0],
		TaskToolDefinitions()[1],
		TaskToolDefinitions()[2],
		TaskToolDefinitions()[3],
		// Team (5)
		TeamToolDefinitions()[0],
		TeamToolDefinitions()[1],
		TeamToolDefinitions()[2],
		TeamToolDefinitions()[3],
		TeamToolDefinitions()[4],
		// Protocols (2)
		ProtocolToolDefinitions()[0],
		ProtocolToolDefinitions()[1],
		// Misc (2)
		ToolDefinition{
			Name:        "idle",
			Description: "Enter idle state.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		ToolDefinition{
			Name:        "claim_task",
			Description: "Claim a task from the board.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

// === System prompt ===

func BuildSystemPrompt() string {
	return fmt.Sprintf(`You are a coding agent at %s. Use tools to solve tasks.
Prefer task_create/task_update/task_list for multi-step work. Use TodoWrite for short checklists.
Use task for subagent delegation. Use load_skill for specialized knowledge.
Skills: %s`, WorkDir, SKILLS.GetDescriptions())
}

// === Agent loop ===

func FullAgentLoop(messages []Message, systemPrompt string) error {
	roundsWithoutTodo := 0
	maxRounds := 100

	for round := 0; round < maxRounds; round++ {
		// s06: compression pipeline - microcompact before each LLM call
		MicroCompact(messages)
		if EstimateTokens(messages) > TokenThreshold {
			fmt.Println("[auto-compact triggered]")
			compactMsgs, err := AutoCompact(messages, systemPrompt)
			if err != nil {
				fmt.Printf("Auto-compact error: %v\n", err)
			} else {
				messages = compactMsgs
			}
		}

		// s08: drain background notifications
		notifs := BG.DrainNotifications()
		if len(notifs) > 0 {
			var lines []string
			for _, n := range notifs {
				taskID, _ := n["task_id"].(string)
				status, _ := n["status"].(string)
				result, _ := n["result"].(string)
				lines = append(lines, fmt.Sprintf("[bg:%s] %s: %s", taskID, status, result))
			}
			messages = append(messages, Message{
				Role:    "user",
				Content: "<background-results>\n" + strings.Join(lines, "\n") + "\n</background-results>",
			})
		}

		// s09: check lead inbox
		inbox := BUS.ReadInbox("lead")
		if len(inbox) > 0 {
			data, _ := json.MarshalIndent(inbox, "", "  ")
			messages = append(messages, Message{
				Role:    "user",
				Content: "<inbox>" + string(data) + "</inbox>",
			})
		}

		// LLM call
		resp, err := SendMessage(systemPrompt, messages, FullToolDefinitions())
		if err != nil {
			return fmt.Errorf("LLM call failed: %w", err)
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})

		if resp.StopReason != "tool_use" {
			return nil
		}

		// Tool execution
		results := ExecuteAllTools(resp.Content, fullRegistry)
		usedTodo := false
		manualCompress := false

		for _, block := range resp.Content {
			toolUse, ok := block.(map[string]interface{})
			if !ok || toolUse["type"] != "tool_use" {
				continue
			}
			name, _ := toolUse["name"].(string)
			if name == "TodoWrite" {
				usedTodo = true
			}
			if name == "compress" {
				manualCompress = true
			}
		}

		// s03: nag reminder
		roundsWithoutTodo = 0
		if usedTodo {
			roundsWithoutTodo = 0
		} else {
			roundsWithoutTodo++
		}
		if TODO.HasOpenItems() && roundsWithoutTodo >= 3 {
			results = append(results, map[string]interface{}{
				"type":  "text",
				"text":  "<reminder>Update your todos.</reminder>",
			})
		}

		messages = append(messages, Message{Role: "user", Content: results})

		// s06: manual compress
		if manualCompress {
			fmt.Println("[manual compact]")
			compactMsgs, err := AutoCompact(messages, systemPrompt)
			if err != nil {
				fmt.Printf("Compact error: %v\n", err)
			} else {
				messages = compactMsgs
			}
			return nil
		}
	}

	return fmt.Errorf("max iterations reached")
}

// === REPL ===

func RunFullREPL() {
	InitGlobals()
	fullRegistry = buildFullRegistry()

	systemPrompt := BuildSystemPrompt()
	var messages []Message

	fmt.Println("s_full agent - type 'q' to exit, /compact /tasks /team /inbox for commands")
	fmt.Printf("System prompt (excerpt): %s...\n\n", truncate(systemPrompt, 200))

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\033[36ms_full >> \033[0m")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" || strings.ToLower(input) == "q" {
			break
		}

		switch input {
		case "/compact":
			fmt.Println("[manual compact via /compact]")
			compactMsgs, err := AutoCompact(messages, systemPrompt)
			if err != nil {
				fmt.Printf("Compact error: %v\n", err)
			} else {
				messages = compactMsgs
			}
			continue
		case "/tasks":
			fmt.Println(TASK_MGR.ListAll())
			continue
		case "/team":
			fmt.Println(TEAM.ListAll())
			continue
		case "/inbox":
			inbox := BUS.ReadInbox("lead")
			data, _ := json.MarshalIndent(inbox, "", "  ")
			fmt.Println(string(data))
			continue
		}

		messages = append(messages, Message{Role: "user", Content: input})

		if err := FullAgentLoop(messages, systemPrompt); err != nil {
			fmt.Printf("Agent error: %v\n", err)
			break
		}

		// Print last response
		PrintLastResponse(messages)
		fmt.Println()
	}
}

// === Main ===

func main() {
	RunFullREPL()
}
