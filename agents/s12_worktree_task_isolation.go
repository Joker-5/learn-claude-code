// s12_worktree_task_isolation.go - Directory isolation via git worktrees.
package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const WorktreesDir = ".worktrees"

// detectRepoRoot finds the git repo root for the given working directory.
func DetectRepoRoot(cwd string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return cwd
	}
	root := strings.TrimSpace(string(out))
	if root != "" && !strings.HasPrefix(root, "fatal") {
		return root
	}
	return cwd
}

// --- EventBus ---

type WTEvent struct {
	Event     string                 `json:"event"`
	Timestamp float64                `json:"ts"`
	Task      map[string]interface{} `json:"task,omitempty"`
	Worktree  map[string]interface{} `json:"worktree,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// EventBus publishes append-only lifecycle events.
type EventBus struct {
	mu  sync.Mutex
	path string
}

func NewEventBus(path string) *EventBus {
	if path == "" {
		path = filepath.Join(WorkDir, WorktreesDir, "events.jsonl")
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f.Close()
	return &EventBus{path: path}
}

func (b *EventBus) emit(event string, task map[string]interface{}, worktree map[string]interface{}, errStr string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	evt := WTEvent{
		Event:     event,
		Timestamp: float64(time.Now().Unix()),
		Task:      task,
		Worktree:  worktree,
	}
	if errStr != "" {
		evt.Error = errStr
	}
	data, _ := json.Marshal(evt)
	f, err := os.OpenFile(b.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	f.Write(data)
	f.WriteString("\n")
	f.Close()
}

func (b *EventBus) listRecent(limit int) string {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	data, err := os.ReadFile(b.path)
	if err != nil {
		return "[]"
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	start := 0
	if len(lines) > limit {
		start = len(lines) - limit
	}

	var events []WTEvent
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var evt WTEvent
		if json.Unmarshal([]byte(line), &evt) == nil {
			events = append(events, evt)
		}
	}
	result, _ := json.MarshalIndent(events, "", "  ")
	return string(result)
}

// --- WorktreeIndex ---

type WorktreeEntry struct {
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	Branch    string  `json:"branch"`
	TaskID    int     `json:"task_id,omitempty"`
	Status    string  `json:"status"` // active, kept, removed
	CreatedAt float64 `json:"created_at,omitempty"`
	KeptAt    float64 `json:"kept_at,omitempty"`
	RemovedAt float64 `json:"removed_at,omitempty"`
}

type WorktreeIndex struct {
	Worktrees []WorktreeEntry `json:"worktrees"`
}

// --- WorktreeManager ---

var worktreeNameRegex = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

type WorktreeManager struct {
	repoRoot     string
	tasks        *TaskManager
	events       *EventBus
	dir          string
	indexPath    string
	gitAvailable bool
}

func NewWorktreeManager(repoRoot string, tasks *TaskManager, events *EventBus) *WorktreeManager {
	dir := filepath.Join(repoRoot, WorktreesDir)
	indexPath := filepath.Join(dir, "index.json")
	os.MkdirAll(dir, 0755)

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		data, _ := json.MarshalIndent(WorktreeIndex{Worktrees: []WorktreeEntry{}}, "", "  ")
		os.WriteFile(indexPath, data, 0644)
	}

	// Check git availability
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = repoRoot
	available := cmd.Run() == nil

	return &WorktreeManager{
		repoRoot:     repoRoot,
		tasks:        tasks,
		events:       events,
		dir:          dir,
		indexPath:    indexPath,
		gitAvailable: available,
	}
}

func (m *WorktreeManager) loadIndex() (*WorktreeIndex, error) {
	data, err := os.ReadFile(m.indexPath)
	if err != nil {
		return nil, err
	}
	var idx WorktreeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func (m *WorktreeManager) saveIndex(idx *WorktreeIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.indexPath, data, 0644)
}

func (m *WorktreeManager) find(name string) *WorktreeEntry {
	idx, err := m.loadIndex()
	if err != nil {
		return nil
	}
	for i := range idx.Worktrees {
		if idx.Worktrees[i].Name == name {
			return &idx.Worktrees[i]
		}
	}
	return nil
}

func (m *WorktreeManager) runGit(args ...string) (string, error) {
	if !m.gitAvailable {
		return "", fmt.Errorf("not in a git repository. worktree tools require git")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = m.repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Create creates a new git worktree bound to an optional task.
func (m *WorktreeManager) Create(name string, taskID int, baseRef string) (string, error) {
	if !worktreeNameRegex.MatchString(name) {
		return "", fmt.Errorf("invalid worktree name. Use 1-40 chars: letters, numbers, ., _, -")
	}
	if m.find(name) != nil {
		return "", fmt.Errorf("worktree '%s' already exists", name)
	}
	if taskID != 0 && !m.tasks.Exists(taskID) {
		return "", fmt.Errorf("task %d not found", taskID)
	}

	if baseRef == "" {
		baseRef = "HEAD"
	}

	path := filepath.Join(m.dir, name)
	branch := fmt.Sprintf("wt/%s", name)

	m.events.emit("worktree.create.before",
		map[string]interface{}{"id": taskID},
		map[string]interface{}{"name": name, "base_ref": baseRef},
		"",
	)

	_, err := m.runGit("worktree", "add", "-b", branch, path, baseRef)
	if err != nil {
		m.events.emit("worktree.create.failed",
			map[string]interface{}{"id": taskID},
			map[string]interface{}{"name": name, "base_ref": baseRef},
			err.Error(),
		)
		return "", err
	}

	entry := WorktreeEntry{
		Name:      name,
		Path:      path,
		Branch:    branch,
		TaskID:    taskID,
		Status:    "active",
		CreatedAt: float64(time.Now().Unix()),
	}

	idx, _ := m.loadIndex()
	idx.Worktrees = append(idx.Worktrees, entry)
	m.saveIndex(idx)

	if taskID != 0 {
		m.tasks.BindWorktree(taskID, name)
	}

	m.events.emit("worktree.create.after",
		map[string]interface{}{"id": taskID},
		map[string]interface{}{"name": name, "path": path, "branch": branch, "status": "active"},
		"",
	)

	data, _ := json.MarshalIndent(entry, "", "  ")
	return string(data), nil
}

// ListAll returns a summary of all tracked worktrees.
func (m *WorktreeManager) ListAll() string {
	idx, err := m.loadIndex()
	if err != nil || len(idx.Worktrees) == 0 {
		return "No worktrees in index."
	}
	var lines []string
	for _, wt := range idx.Worktrees {
		suffix := ""
		if wt.TaskID != 0 {
			suffix = fmt.Sprintf(" task=%d", wt.TaskID)
		}
		lines = append(lines, fmt.Sprintf("[%s] %s -> %s (%s)%s",
			wt.Status, wt.Name, wt.Path, wt.Branch, suffix))
	}
	return strings.Join(lines, "\n")
}

// Status returns git status for a worktree.
func (m *WorktreeManager) Status(name string) string {
	wt := m.find(name)
	if wt == nil {
		return fmt.Sprintf("Error: Unknown worktree '%s'", name)
	}
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		return fmt.Sprintf("Error: Worktree path missing: %s", wt.Path)
	}
	out, _ := m.runGit("-C", wt.Path, "status", "--short", "--branch")
	if out == "" {
		return "Clean worktree"
	}
	return out
}

// Run executes a command in a worktree directory.
func (m *WorktreeManager) Run(name string, command string) string {
	if IsDangerous(command) {
		return "Error: Dangerous command blocked"
	}
	wt := m.find(name)
	if wt == nil {
		return fmt.Sprintf("Error: Unknown worktree '%s'", name)
	}
	if _, err := os.Stat(wt.Path); os.IsNotExist(err) {
		return fmt.Sprintf("Error: Worktree path missing: %s", wt.Path)
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = wt.Path
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return "Error: Command not found"
		}
		if strings.Contains(err.Error(), "timeout") {
			return "Error: Timeout (300s)"
		}
		return fmt.Sprintf("Error: %v", err)
	}
	return truncate(result, MaxOutputLen)
}

// Remove deletes a worktree and optionally marks its task complete.
func (m *WorktreeManager) Remove(name string, force bool, completeTask bool) (string, error) {
	wt := m.find(name)
	if wt == nil {
		return "", fmt.Errorf("unknown worktree '%s'", name)
	}

	m.events.emit("worktree.remove.before",
		map[string]interface{}{"id": wt.TaskID},
		map[string]interface{}{"name": name, "path": wt.Path},
		"",
	)

	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, wt.Path)

	if _, err := m.runGit(args...); err != nil {
		m.events.emit("worktree.remove.failed",
			map[string]interface{}{"id": wt.TaskID},
			map[string]interface{}{"name": name, "path": wt.Path},
			err.Error(),
		)
		return "", err
	}

	if completeTask && wt.TaskID != 0 {
		before := m.tasks.GetTask(wt.TaskID)
		m.tasks.Update(wt.TaskID, "completed", nil, nil)
		m.tasks.UnbindWorktree(wt.TaskID)
		m.events.emit("task.completed",
			map[string]interface{}{"id": wt.TaskID, "subject": before, "status": "completed"},
			map[string]interface{}{"name": name},
			"",
		)
	}

	idx, _ := m.loadIndex()
	for i := range idx.Worktrees {
		if idx.Worktrees[i].Name == name {
			idx.Worktrees[i].Status = "removed"
			idx.Worktrees[i].RemovedAt = float64(time.Now().Unix())
		}
	}
	m.saveIndex(idx)

	m.events.emit("worktree.remove.after",
		map[string]interface{}{"id": wt.TaskID},
		map[string]interface{}{"name": name, "path": wt.Path, "status": "removed"},
		"",
	)

	return fmt.Sprintf("Removed worktree '%s'", name), nil
}

// Keep marks a worktree as kept in lifecycle state without removing it.
func (m *WorktreeManager) Keep(name string) (string, error) {
	wt := m.find(name)
	if wt == nil {
		return "", fmt.Errorf("unknown worktree '%s'", name)
	}

	idx, _ := m.loadIndex()
	var kept *WorktreeEntry
	for i := range idx.Worktrees {
		if idx.Worktrees[i].Name == name {
			idx.Worktrees[i].Status = "kept"
			idx.Worktrees[i].KeptAt = float64(time.Now().Unix())
			kept = &idx.Worktrees[i]
		}
	}
	m.saveIndex(idx)

	m.events.emit("worktree.keep",
		map[string]interface{}{"id": wt.TaskID},
		map[string]interface{}{"name": name, "path": wt.Path, "status": "kept"},
		"",
	)

	data, _ := json.MarshalIndent(kept, "", "  ")
	return string(data), nil
}

// WorktreeToolDefinitions returns all worktree-related tool schemas.
func WorktreeToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "worktree_create",
			Description: "Create a git worktree and optionally bind it to a task.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string"},
					"task_id": map[string]interface{}{"type": "integer"},
					"base_ref": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "worktree_list",
			Description: "List worktrees tracked in .worktrees/index.json.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "worktree_status",
			Description: "Show git status for one worktree.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "worktree_run",
			Description: "Run a shell command in a named worktree directory.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string"},
					"command": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name", "command"},
			},
		},
		{
			Name:        "worktree_remove",
			Description: "Remove a worktree and optionally mark its bound task completed.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":         map[string]interface{}{"type": "string"},
					"force":        map[string]interface{}{"type": "boolean"},
					"complete_task": map[string]interface{}{"type": "boolean"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "worktree_keep",
			Description: "Mark a worktree as kept in lifecycle state without removing it.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "worktree_events",
			Description: "List recent worktree/task lifecycle events from .worktrees/events.jsonl.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}
}

// ExtendedTaskManager adds worktree binding methods.
func (m *TaskManager) BindWorktree(taskID int, worktree string) string {
	task, err := m.load(taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	task.Worktree = worktree
	if task.Status == "pending" {
		task.Status = "in_progress"
	}
	task.UpdatedAt = float64(time.Now().Unix())
	m.save(task)
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data)
}

func (m *TaskManager) UnbindWorktree(taskID int) string {
	task, err := m.load(taskID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	task.Worktree = ""
	task.UpdatedAt = float64(time.Now().Unix())
	m.save(task)
	data, _ := json.MarshalIndent(task, "", "  ")
	return string(data)
}

func (m *TaskManager) GetTask(taskID int) string {
	task, err := m.load(taskID)
	if err != nil {
		return ""
	}
	return task.Subject
}

func (m *TaskManager) Exists(taskID int) bool {
	_, err := m.load(taskID)
	return err == nil
}

// TaskWithWorktree extends Task with worktree binding.
type TaskWithWorktree struct {
	Task
	Worktree string `json:"worktree"`
}
