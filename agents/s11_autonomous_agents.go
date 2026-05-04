// s11_autonomous_agents.go - Autonomous agents that find their own work via idle polling.
package agents

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	PollInterval = 5  // seconds
	IdleTimeout  = 60 // seconds
)

// MakeIdentityBlock creates an identity message for re-injection after compression.
func MakeIdentityBlock(name string, role string, teamName string) Message {
	return Message{
		Role:    "user",
		Content: fmt.Sprintf("<identity>You are '%s', role: %s, team: %s.</identity>", name, role, teamName),
	}
}

// ClaimTask atomically claims a task for an owner.
func ClaimTask(taskMgr *TaskManager, taskID int, owner string) string {
	return taskMgr.Claim(taskID, owner)
}

// ScanUnclaimedTasks returns pending tasks with no owner and no blockedBy.
func ScanUnclaimedTasks(taskMgr *TaskManager) []*Task {
	return taskMgr.ScanUnclaimed()
}

// AutonomousLoop runs an autonomous teammate with idle polling and auto-claim.
// This is integrated into TeammateManager.loop() in s09.
// These are helper functions for the s11 pattern.
func (m *TeammateManager) idlePoll(name string, messages []Message, stopChan chan struct{}) bool {
	pollInterval := time.Duration(PollInterval) * time.Second
	idleTimeout := time.Duration(IdleTimeout) * time.Second
	start := time.Now()

	for time.Since(start) < idleTimeout {
		select {
		case <-stopChan:
			return false
		case <-time.After(pollInterval):
		}

		// Check inbox
		inbox := m.bus.ReadInbox(name)
		if len(inbox) > 0 {
			for _, msg := range inbox {
				if msg.Type == "shutdown_request" {
					m.setStatus(name, "shutdown")
					return false
				}
				content := jsonMarshal(msg)
				messages = append(messages, Message{Role: "user", Content: content})
			}
			m.setStatus(name, "working")
			return true
		}

		// Check for unclaimed tasks
		unclaimed := m.taskMgr.ScanUnclaimed()
		if len(unclaimed) > 0 {
			task := unclaimed[0]
			m.taskMgr.Claim(task.ID, name)

			// Identity re-injection
			if len(messages) <= 3 {
				messages = append(messages, Message{
					Role:    "user",
					Content: fmt.Sprintf("<identity>You are '%s', role: %s, team: %s.</identity>", name, name, m.config.TeamName),
				})
				messages = append(messages, Message{
					Role:    "assistant",
					Content: fmt.Sprintf("I am %s. Continuing.", name),
				})
			}
			messages = append(messages, Message{
				Role:    "user",
				Content: fmt.Sprintf("<auto-claimed>Task #%d: %s\n%s</auto-claimed>", task.ID, task.Subject, task.Description),
			})
			messages = append(messages, Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Claimed task #%d. Working on it.", task.ID),
			})
			m.setStatus(name, "working")
			return true
		}
	}
	return false
}

func jsonMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// IdleToolDefinition returns the tool schema for the idle tool.
func IdleToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "idle",
		Description: "Enter idle state.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		},
	}
}
