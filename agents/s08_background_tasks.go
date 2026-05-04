// s08_background_tasks.go - Background execution with goroutine/channel-based notifications.
package agents

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// BGTask represents a background task.
type BGTask struct {
	ID      string
	Status  string // running, completed, error
	Command string
	Result  string
}

// BackgroundManager manages background task execution.
type BackgroundManager struct {
	mu      sync.Mutex
	tasks   map[string]*BGTask
	results chan map[string]interface{} // notification channel
}

func NewBackgroundManager() *BackgroundManager {
	return &BackgroundManager{
		tasks:   make(map[string]*BGTask),
		results: make(chan map[string]interface{}, 100),
	}
}

// Run starts a background command and returns immediately with a task ID.
func (m *BackgroundManager) Run(command string, timeout int) string {
	m.mu.Lock()
	taskID := uuid.New().String()[:8]
	m.tasks[taskID] = &BGTask{
		ID:      taskID,
		Status:  "running",
		Command: command,
		Result:  "",
	}
	m.mu.Unlock()

	if timeout <= 0 {
		timeout = 120
	}

	go m.exec(taskID, command, timeout)
	return fmt.Sprintf("Background task %s started: %s", taskID, truncate(command, 80))
}

func (m *BackgroundManager) exec(taskID string, command string, timeout int) {
	m.mu.Lock()
	task := m.tasks[taskID]
	m.mu.Unlock()

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = WorkDir
	out, err := cmd.CombinedOutput()

	m.mu.Lock()
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") ||
			strings.Contains(err.Error(), "not found") {
			task.Status = "error"
			task.Result = fmt.Sprintf("Command not found: %s", command)
		} else {
			task.Status = "error"
			task.Result = fmt.Sprintf("%v", err)
		}
	} else {
		task.Status = "completed"
		result := strings.TrimSpace(string(out))
		task.Result = truncate(result, MaxOutputLen)
		if task.Result == "" {
			task.Result = "(no output)"
		}
	}
	m.mu.Unlock()

	// Push notification
	select {
	case m.results <- map[string]interface{}{
		"task_id": taskID,
		"status":  task.Status,
		"result":  truncate(task.Result, 500),
	}:
	default:
		// Channel full, skip notification
	}
}

// Check returns the status of a specific task or all tasks.
func (m *BackgroundManager) Check(taskID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if taskID != "" {
		task, ok := m.tasks[taskID]
		if !ok {
			return fmt.Sprintf("Unknown: %s", taskID)
		}
		result := task.Result
		if task.Status == "running" {
			result = "(running)"
		}
		return fmt.Sprintf("[%s] %s", task.Status, result)
	}

	// List all
	var lines []string
	for id, task := range m.tasks {
		cmd := truncate(task.Command, 60)
		lines = append(lines, fmt.Sprintf("%s: [%s] %s", id, task.Status, cmd))
	}
	if len(lines) == 0 {
		return "No bg tasks."
	}
	return strings.Join(lines, "\n")
}

// DrainNotifications atomically returns and clears all pending notifications.
func (m *BackgroundManager) DrainNotifications() []map[string]interface{} {
	var notifs []map[string]interface{}
	for {
		select {
		case n := <-m.results:
			notifs = append(notifs, n)
		default:
			return notifs
		}
	}
}

// BGToolDefinitions returns background task tool schemas.
func BGToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "background_run",
			Description: "Run command in background thread.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
					"timeout": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "check_background",
			Description: "Check background task status.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
