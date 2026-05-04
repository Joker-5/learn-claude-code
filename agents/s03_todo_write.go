// s03_todo_write.go - In-conversation todo tracking with nag reminder.
package agents

import (
	"fmt"
	"strings"
	"sync"
)

// TodoItem represents a single todo item.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"` // pending, in_progress, completed
	ActiveForm string `json:"activeForm"`
}

// TodoManager handles todo list management with thread safety.
type TodoManager struct {
	mu    sync.RWMutex
	items []TodoItem
}

// NewTodoManager creates a new TodoManager.
func NewTodoManager() *TodoManager {
	return &TodoManager{}
}

// Update replaces the entire todo list with validation.
func (m *TodoManager) Update(items []map[string]interface{}) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	validated := make([]TodoItem, 0, len(items))
	inProgress := 0

	for i, item := range items {
		content, ok := item["content"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("item %d: content required", i)
		}

		status, ok := item["status"].(string)
		if !ok {
			status = "pending"
		}
		status = strings.ToLower(status)
		if status != "pending" && status != "in_progress" && status != "completed" {
			return "", fmt.Errorf("item %d: invalid status '%s'", i, status)
		}

		activeForm, ok := item["activeForm"].(string)
		if !ok || strings.TrimSpace(activeForm) == "" {
			return "", fmt.Errorf("item %d: activeForm required", i)
		}

		if status == "in_progress" {
			inProgress++
		}

		validated = append(validated, TodoItem{
			Content:    strings.TrimSpace(content),
			Status:     status,
			ActiveForm: strings.TrimSpace(activeForm),
		})
	}

	if len(validated) > 20 {
		return "", fmt.Errorf("max 20 todos")
	}
	if inProgress > 1 {
		return "", fmt.Errorf("only one in_progress allowed")
	}

	m.items = validated
	return m.Render(), nil
}

// Render returns a human-readable string representation of the todo list.
func (m *TodoManager) Render() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.items) == 0 {
		return "No todos."
	}
	var lines []string
	for _, item := range m.items {
		marker := map[string]string{
			"completed":  "[x]",
			"in_progress": "[>]",
			"pending":    "[ ]",
		}[item.Status]
		if marker == "" {
			marker = "[?]"
		}
		suffix := ""
		if item.Status == "in_progress" {
			suffix = " <- " + item.ActiveForm
		}
		lines = append(lines, fmt.Sprintf("%s %s%s", marker, item.Content, suffix))
	}
	done := 0
	for _, t := range m.items {
		if t.Status == "completed" {
			done++
		}
	}
	lines = append(lines, fmt.Sprintf("\n(%d/%d completed)", done, len(m.items)))
	return strings.Join(lines, "\n")
}

// HasOpenItems returns true if there are uncompleted todos.
func (m *TodoManager) HasOpenItems() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.items {
		if item.Status != "completed" {
			return true
		}
	}
	return false
}

// Items returns a copy of current items.
func (m *TodoManager) Items() []TodoItem {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]TodoItem, len(m.items))
	copy(result, m.items)
	return result
}

// TodoToolDefinition returns the tool schema for TodoWrite.
func TodoToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "TodoWrite",
		Description: "Update task tracking list.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"items": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"content":     map[string]interface{}{"type": "string"},
							"status":      map[string]interface{}{"type": "string", "enum": []interface{}{"pending", "in_progress", "completed"}},
							"activeForm":  map[string]interface{}{"type": "string"},
						},
						"required": []string{"content", "status", "activeForm"},
					},
				},
			},
			"required": []string{"items"},
		},
	}
}
