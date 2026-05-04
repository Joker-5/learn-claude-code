// s09_agent_teams.go - Multi-agent coordination via file-based JSONL inboxes.
package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const TeamDir = ".team"
const InboxDir = TeamDir + "/inbox"

var ValidMsgTypes = map[string]bool{
	"message":               true,
	"broadcast":             true,
	"shutdown_request":       true,
	"shutdown_response":      true,
	"plan_approval_response": true,
}

// TeamMessage represents a message between agents.
type TeamMessage struct {
	Type      string                 `json:"type"`
	From      string                 `json:"from"`
	Content   string                 `json:"content"`
	Timestamp float64                `json:"timestamp"`
	Extra     map[string]interface{} `json:",omitempty"`
}

// MessageBus manages JSONL inbox files for inter-agent communication.
type MessageBus struct {
	mu       sync.Mutex
	inboxDir string
}

func NewMessageBus(inboxDir string) *MessageBus {
	if inboxDir == "" {
		inboxDir = filepath.Join(WorkDir, InboxDir)
	}
	os.MkdirAll(inboxDir, 0755)
	return &MessageBus{inboxDir: inboxDir}
}

func (m *MessageBus) inboxPath(name string) string {
	return filepath.Join(m.inboxDir, name+".jsonl")
}

// Send delivers a message to a teammate's inbox.
func (m *MessageBus) Send(sender string, to string, content string, msgType string, extra map[string]interface{}) string {
	msg := TeamMessage{
		Type:      msgType,
		From:      sender,
		Content:   content,
		Timestamp: float64(time.Now().Unix()),
	}
	if extra != nil {
		msg.Extra = extra
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	f, err := os.OpenFile(m.inboxPath(to), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	f.Write(data)
	f.WriteString("\n")
	f.Close()

	return fmt.Sprintf("Sent %s to %s", msgType, to)
}

// ReadInbox atomically reads and clears a teammate's inbox.
func (m *MessageBus) ReadInbox(name string) []TeamMessage {
	path := m.inboxPath(name)
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}

	var msgs []TeamMessage
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var msg TeamMessage
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			msgs = append(msgs, msg)
		}
	}

	// Clear the inbox
	os.WriteFile(path, []byte{}, 0644)
	return msgs
}

// Broadcast sends a message to all teammates except the sender.
func (m *MessageBus) Broadcast(sender string, content string, names []string) string {
	count := 0
	for _, n := range names {
		if n != sender {
			m.Send(sender, n, content, "broadcast", nil)
			count++
		}
	}
	return fmt.Sprintf("Broadcast to %d teammates", count)
}

// Teammate represents a team member.
type Teammate struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"` // idle, working, shutdown
}

// TeamConfig holds team configuration.
type TeamConfig struct {
	TeamName string      `json:"team_name"`
	Members  []Teammate  `json:"members"`
}

// TeammateManager manages persistent team members.
type TeammateManager struct {
	mu         sync.Mutex
	bus        *MessageBus
	taskMgr    *TaskManager
	teamDir    string
	configPath string
	config     *TeamConfig
	stopChans  map[string]chan struct{}
}

func NewTeammateManager(bus *MessageBus, taskMgr *TaskManager) *TeammateManager {
	teamDir := filepath.Join(WorkDir, TeamDir)
	os.MkdirAll(teamDir, 0755)
	configPath := filepath.Join(teamDir, "config.json")

	config := &TeamConfig{TeamName: "default", Members: []Teammate{}}
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, config)
	}

	return &TeammateManager{
		bus:        bus,
		taskMgr:    taskMgr,
		teamDir:    teamDir,
		configPath: configPath,
		config:     config,
		stopChans:  make(map[string]chan struct{}),
	}
}

func (m *TeammateManager) saveConfig() {
	data, _ := json.MarshalIndent(m.config, "", "  ")
	os.WriteFile(m.configPath, data, 0644)
}

func (m *TeammateManager) find(name string) *Teammate {
	for i := range m.config.Members {
		if m.config.Members[i].Name == name {
			return &m.config.Members[i]
		}
	}
	return nil
}

func (m *TeammateManager) setStatus(name string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.find(name)
	if t != nil {
		t.Status = status
		m.saveConfig()
	}
}

// Spawn creates a new teammate running in a goroutine.
func (m *TeammateManager) Spawn(name string, role string, prompt string) string {
	m.mu.Lock()
	member := m.find(name)
	if member != nil {
		if member.Status != "idle" && member.Status != "shutdown" {
			return fmt.Sprintf("Error: '%s' is currently %s", name, member.Status)
		}
		member.Status = "working"
		member.Role = role
	} else {
		member = &Teammate{Name: name, Role: role, Status: "working"}
		m.config.Members = append(m.config.Members, *member)
	}
	m.saveConfig()
	stopChan := make(chan struct{})
	m.stopChans[name] = stopChan
	m.mu.Unlock()

	go m.loop(name, role, prompt, stopChan)
	return fmt.Sprintf("Spawned '%s' (role: %s)", name, role)
}

func (m *TeammateManager) loop(name string, role string, prompt string, stopChan chan struct{}) {
	systemPrompt := fmt.Sprintf("You are '%s', role: %s, team: %s, at %s. Use idle when done. You may auto-claim tasks.",
		name, role, m.config.TeamName, WorkDir)

	registry := NewToolRegistry()
	registry.Register("bash", func(args map[string]interface{}) string {
		cmd, _ := args["command"].(string)
		return RunBash(cmd)
	})
	registry.Register("read_file", func(args map[string]interface{}) string {
		path, _ := args["path"].(string)
		return RunRead(path, 0)
	})
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
	registry.Register("send_message", func(args map[string]interface{}) string {
		to, _ := args["to"].(string)
		content, _ := args["content"].(string)
		return m.bus.Send(name, to, content, "message", nil)
	})
	registry.Register("idle", func(args map[string]interface{}) string {
		return "Entering idle phase."
	})
	registry.Register("claim_task", func(args map[string]interface{}) string {
		taskID, _ := args["task_id"].(int)
		return m.taskMgr.Claim(taskID, name)
	})

	tools := []ToolDefinition{
		ToolDefinition{Name: "bash", Description: "Run command.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string"},
			},
			"required": []string{"command"},
		}},
		ToolDefinition{Name: "read_file", Description: "Read file.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path"},
		}},
		ToolDefinition{Name: "write_file", Description: "Write file.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path", "content"},
		}},
		ToolDefinition{Name: "edit_file", Description: "Edit file.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":     map[string]interface{}{"type": "string"},
				"old_text": map[string]interface{}{"type": "string"},
				"new_text": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path", "old_text", "new_text"},
		}},
		ToolDefinition{Name: "send_message", Description: "Send message.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"to":      map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []string{"to", "content"},
		}},
		ToolDefinition{Name: "idle", Description: "Signal no more work.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		}},
		ToolDefinition{Name: "claim_task", Description: "Claim task by ID.", InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"task_id"},
		}},
	}

	messages := []Message{{Role: "user", Content: prompt}}
	idleRequested := false
	maxWorkRounds := 50

	// WORK PHASE
	for round := 0; round < maxWorkRounds; round++ {
		// Check stop channel
		select {
		case <-stopChan:
			m.setStatus(name, "shutdown")
			return
		default:
		}

		// Process inbox
		inbox := m.bus.ReadInbox(name)
		for _, msg := range inbox {
			if msg.Type == "shutdown_request" {
				m.setStatus(name, "shutdown")
				return
			}
			content, _ := json.Marshal(msg)
			messages = append(messages, Message{Role: "user", Content: string(content)})
		}

		resp, err := SendMessage(systemPrompt, messages, tools)
		if err != nil {
			m.setStatus(name, "shutdown")
			return
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})
		if resp.StopReason != "tool_use" {
			break
		}

		idleRequested = false
		results := ExecuteAllTools(resp.Content, registry)
		for i, block := range resp.Content {
			toolUse, ok := block.(map[string]interface{})
			if !ok || toolUse["type"] != "tool_use" {
				continue
			}
			tname, _ := toolUse["name"].(string)
			if tname == "idle" {
				idleRequested = true
			}
			output := ""
			if i < len(results) {
				if r, ok := results[i].(map[string]interface{}); ok {
					output, _ = r["content"].(string)
				}
			}
			fmt.Printf("  [%s] %s: %s\n", name, tname, truncate(output, 120))
		}
		messages = append(messages, Message{Role: "user", Content: results})
		if idleRequested {
			break
		}
	}

	// IDLE PHASE: poll for messages and unclaimed tasks
	m.setStatus(name, "idle")
	pollInterval := 5 * time.Second
	idleTimeout := 60 * time.Second
	start := time.Now()
	for time.Since(start) < idleTimeout {
		time.Sleep(pollInterval)

		select {
		case <-stopChan:
			m.setStatus(name, "shutdown")
			return
		default:
		}

		inbox := m.bus.ReadInbox(name)
		if len(inbox) > 0 {
			for _, msg := range inbox {
				if msg.Type == "shutdown_request" {
					m.setStatus(name, "shutdown")
					return
				}
				content, _ := json.Marshal(msg)
				messages = append(messages, Message{Role: "user", Content: string(content)})
			}
			m.setStatus(name, "working")
			return
		}

		unclaimed := m.taskMgr.ScanUnclaimed()
		if len(unclaimed) > 0 {
			task := unclaimed[0]
			m.taskMgr.Claim(task.ID, name)

			// Identity re-injection for compressed contexts
			if len(messages) <= 3 {
				messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("<identity>You are '%s', role: %s, team: %s.</identity>", name, role, m.config.TeamName)})
				messages = append(messages, Message{Role: "assistant", Content: fmt.Sprintf("I am %s. Continuing.", name)})
			}
			messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("<auto-claimed>Task #%d: %s\n%s</auto-claimed>", task.ID, task.Subject, task.Description)})
			messages = append(messages, Message{Role: "assistant", Content: fmt.Sprintf("Claimed task #%d. Working on it.", task.ID)})
			m.setStatus(name, "working")
			return
		}
	}

	m.setStatus(name, "shutdown")
}

// ListAll returns a human-readable team status.
func (m *TeammateManager) ListAll() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.config.Members) == 0 {
		return "No teammates."
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("Team: %s", m.config.TeamName))
	for _, member := range m.config.Members {
		lines = append(lines, fmt.Sprintf("  %s (%s): %s", member.Name, member.Role, member.Status))
	}
	return strings.Join(lines, "\n")
}

// MemberNames returns all teammate names.
func (m *TeammateManager) MemberNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for _, m := range m.config.Members {
		names = append(names, m.Name)
	}
	return names
}

// TeamToolDefinitions returns all team-related tool schemas.
func TeamToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "spawn_teammate",
			Description: "Spawn a persistent autonomous teammate.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":   map[string]interface{}{"type": "string"},
					"role":   map[string]interface{}{"type": "string"},
					"prompt": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name", "role", "prompt"},
			},
		},
		{
			Name:        "list_teammates",
			Description: "List all teammates.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "send_message",
			Description: "Send a message to a teammate.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"to":      map[string]interface{}{"type": "string"},
					"content": map[string]interface{}{"type": "string"},
					"msg_type": map[string]interface{}{"type": "string"},
				},
				"required": []string{"to", "content"},
			},
		},
		{
			Name:        "read_inbox",
			Description: "Read and drain the lead's inbox.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "broadcast",
			Description: "Send message to all teammates.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{"type": "string"},
				},
				"required": []string{"content"},
			},
		},
	}
}
