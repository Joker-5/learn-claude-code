// s02_tool_use.go - Multiple tool handlers: bash, read_file, write_file, edit_file.
package agents

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SafePath validates that a path stays within WorkDir.
func SafePath(p string) (string, error) {
	resolved := filepath.Join(WorkDir, p)
	if !strings.HasPrefix(resolved, WorkDir) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return resolved, nil
}

// RunRead reads a file with optional line limit.
func RunRead(path string, limit int) string {
	fp, err := SafePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	if limit > 0 && len(lines) > limit {
		lines = append(lines[:limit], fmt.Sprintf("... (%d more)", len(lines)-limit))
	}
	result := strings.Join(lines, "\n")
	return Truncate(result, MaxOutputLen)
}

// RunWrite writes content to a file.
func RunWrite(path string, content string) string {
	fp, err := SafePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(content), path)
}

// RunEdit replaces exact old_text with new_text in a file.
func RunEdit(path string, oldText string, newText string) string {
	fp, err := SafePath(path)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, oldText) {
		return fmt.Sprintf("Error: Text not found in %s", path)
	}
	newTextCombined := strings.Replace(text, oldText, newText, 1)
	if err := os.WriteFile(fp, []byte(newTextCombined), 0644); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Edited %s", path)
}

// ToolHandler is a function that handles a tool call.
type ToolHandler func(args map[string]interface{}) string

// ToolRegistry maps tool names to handlers.
type ToolRegistry struct {
	handlers map[string]ToolHandler
}

func NewToolRegistry() *ToolRegistry {
	r := &ToolRegistry{handlers: make(map[string]ToolHandler)}
	r.handlers["bash"] = func(args map[string]interface{}) string {
		cmd, _ := args["command"].(string)
		return RunBash(cmd)
	}
	r.handlers["read_file"] = func(args map[string]interface{}) string {
		path, _ := args["path"].(string)
		limit := 0
		if l, ok := args["limit"].(float64); ok {
			limit = int(l)
		}
		return RunRead(path, limit)
	}
	r.handlers["write_file"] = func(args map[string]interface{}) string {
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		return RunWrite(path, content)
	}
	r.handlers["edit_file"] = func(args map[string]interface{}) string {
		path, _ := args["path"].(string)
		oldText, _ := args["old_text"].(string)
		newText, _ := args["new_text"].(string)
		return RunEdit(path, oldText, newText)
	}
	return r
}

func (r *ToolRegistry) Register(name string, handler ToolHandler) {
	r.handlers[name] = handler
}

func (r *ToolRegistry) Handle(name string, args map[string]interface{}) string {
	if h, ok := r.handlers[name]; ok {
		return h(args)
	}
	return fmt.Sprintf("Unknown tool: %s", name)
}

func (r *ToolRegistry) ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
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
		{
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
		{
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
		{
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
	}
}

// ExecuteAllTools dispatches all tool calls from the response.
func ExecuteAllTools(blocks []interface{}, registry *ToolRegistry) []interface{} {
	var results []interface{}
	for _, b := range blocks {
		toolUse, ok := b.(map[string]interface{})
		if !ok || toolUse["type"] != "tool_use" {
			continue
		}
		name, _ := toolUse["name"].(string)
		input, _ := toolUse["input"].(map[string]interface{})
		id, _ := toolUse["id"].(string)

		output := registry.Handle(name, input)
		results = append(results, map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": id,
			"content":     Truncate(output, MaxOutputLen),
		})
	}
	return results
}

// --- fs.FS compatibility shim for go 1.16---
var _ fs.FS = nil
