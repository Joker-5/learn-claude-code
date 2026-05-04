// s07_task_system.go - Persistent task system with JSON file storage and dependency graph.
package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const TasksDir = ".tasks"

// Task represents a persistent task.
type Task struct {
	ID          int       `json:"id"`
	Subject     string    `json:"subject"`
	Description string    `json:"description"`
	Status      string    `json:"status"` // pending, in_progress, completed, deleted
	Owner       string    `json:"owner,omitempty"`
	BlockedBy   []int     `json:"blockedBy"`
	Worktree    string    `json:"worktree,omitempty"`
	CreatedAt   float64   `json:"created_at,omitempty"`
	UpdatedAt   float64   `json:"updated_at,omitempty"`
}

// TaskManager manages a file-based task board.
type TaskManager struct {
	mu  sync.Mutex
	dir string
}

func NewTaskManager(dir string) *TaskManager {
	if dir == "" {
		dir = filepath.Join(WorkDir, TasksDir)
	}
	os.MkdirAll(dir, 0755)
	return &TaskManager{dir: dir}
}

func (m *TaskManager) taskPath(taskID int) string {
	return filepath.Join(m.dir, fmt.Sprintf("task_%d.json", taskID))
}

func (m *TaskManager) maxID() int {
	max := 0
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "task_") {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(e.Name(), "task_%d.json", &id); err == nil && id > max {
			max = id
		}
	}
	return max
}

func (m *TaskManager) load(taskID int) (*Task, error) {
	data, err := os.ReadFile(m.taskPath(taskID))
	if err != nil {
		return nil, fmt.Errorf("task %d not found", taskID)
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (m *TaskManager) save(task *Task) error {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.taskPath(task.ID), data, 0644)
}

// Create adds a new task.
func (m *TaskManager) Create(subject string, description string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := float64(time.Now().Unix())
	task := &Task{
		ID:          m.maxID() + 1,
		Subject:     subject,
		Description: description,
		Status:      "pending",
		Owner:       "",
		BlockedBy:   []int{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := m.save(task); err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// Get returns a task by ID as JSON string.
func (m *TaskManager) Get(taskID int) string {
	task, err := m.load(taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data)
}

// Update modifies a task's status and/or dependencies.
func (m *TaskManager) Update(taskID int, status string, addBlockedBy []int, removeBlockedBy []int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.load(taskID)
	if err != nil {
		return "", err
	}

	if status != "" {
		task.Status = status
		task.UpdatedAt = float64(time.Now().Unix())

		if status == "completed" {
			// Clear this task from all other tasks' blockedBy lists
			entries, _ := os.ReadDir(m.dir)
			for _, e := range entries {
				if e.IsDir() || !strings.HasPrefix(e.Name(), "task_") {
					continue
				}
				var id int
				if _, err := fmt.Sscanf(e.Name(), "task_%d.json", &id); err != nil {
					continue
				}
				other, err := m.load(id)
				if err != nil {
					continue
				}
				changed := false
				newBlocked := make([]int, 0, len(other.BlockedBy))
				for _, b := range other.BlockedBy {
					if b == taskID {
						changed = true
					} else {
						newBlocked = append(newBlocked, b)
					}
				}
				if changed {
					other.BlockedBy = newBlocked
					m.save(other)
				}
			}
		}

		if status == "deleted" {
			os.Remove(m.taskPath(taskID))
			return fmt.Sprintf("Task %d deleted", taskID), nil
		}
	}

	if addBlockedBy != nil {
		seen := make(map[int]bool)
		for _, b := range task.BlockedBy {
			seen[b] = true
		}
		for _, b := range addBlockedBy {
			if !seen[b] {
				task.BlockedBy = append(task.BlockedBy, b)
				seen[b] = true
			}
		}
	}

	if removeBlockedBy != nil {
		blocked := make(map[int]bool)
		for _, b := range removeBlockedBy {
			blocked[b] = true
		}
		newBlocked := make([]int, 0, len(task.BlockedBy))
		for _, b := range task.BlockedBy {
			if !blocked[b] {
				newBlocked = append(newBlocked, b)
			}
		}
		task.BlockedBy = newBlocked
	}

	m.save(task)
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data), nil
}

// ListAll returns a human-readable task board.
func (m *TaskManager) ListAll() string {
	entries, err := os.ReadDir(m.dir)
	if err != nil || len(entries) == 0 {
		return "No tasks."
	}

	var tasks []*Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "task_") {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(e.Name(), "task_%d.json", &id); err != nil {
			continue
		}
		task, err := m.load(id)
		if err != nil {
			continue
		}
		tasks = append(tasks, task)
	}

	if len(tasks) == 0 {
		return "No tasks."
	}

	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })

	var lines []string
	for _, t := range tasks {
		marker := map[string]string{
			"completed":  "[x]",
			"in_progress": "[>]",
			"pending":  "[ ]",
		}[t.Status]
		if marker == "" {
			marker = "[?]"
		}
		owner := ""
		if t.Owner != "" {
			owner = " @" + t.Owner
		}
		blocked := ""
		if len(t.BlockedBy) > 0 {
			blocked = fmt.Sprintf(" (blocked by: %v)", t.BlockedBy)
		}
		lines = append(lines, fmt.Sprintf("%s #%d: %s%s%s", marker, t.ID, t.Subject, owner, blocked))
	}
	return strings.Join(lines, "\n")
}

// Claim assigns a task to an owner.
func (m *TaskManager) Claim(taskID int, owner string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.load(taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	task.Owner = owner
	task.Status = "in_progress"
	task.UpdatedAt = float64(time.Now().Unix())
	m.save(task)
	return fmt.Sprintf("Claimed task #%d for %s", taskID, owner)
}

// ScanUnclaimed returns pending tasks with no owner and no blockedBy.
func (m *TaskManager) ScanUnclaimed() []*Task {
	entries, _ := os.ReadDir(m.dir)
	var result []*Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "task_") {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(e.Name(), "task_%d.json", &id); err != nil {
			continue
		}
		task, err := m.load(id)
		if err != nil {
			continue
		}
		if task.Status == "pending" && task.Owner == "" && len(task.BlockedBy) == 0 {
			result = append(result, task)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// TaskToolDefinitions returns all task-related tool schemas.
func TaskToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "task_create",
			Description: "Create a persistent file task.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subject":     map[string]interface{}{"type": "string"},
					"description": map[string]interface{}{"type": "string"},
				},
				"required": []string{"subject"},
			},
		},
		{
			Name:        "task_get",
			Description: "Get task details by ID.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "task_update",
			Description: "Update task status or dependencies.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task_id":        map[string]interface{}{"type": "integer"},
					"status":         map[string]interface{}{"type": "string", "enum": []interface{}{"pending", "in_progress", "completed", "deleted"}},
					"add_blocked_by": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}},
					"remove_blocked_by": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}},
				},
				"required": []string{"task_id"},
			},
		},
		{
			Name:        "task_list",
			Description: "List all tasks.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
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
