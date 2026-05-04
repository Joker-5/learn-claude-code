// s10_team_protocols.go - Structured handshakes for shutdown and plan approval.
package agents

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ShutdownRequest tracks a shutdown request state.
type ShutdownRequest struct {
	Target  string `json:"target"`
	Status  string `json:"status"` // pending, approved, rejected
}

// PlanRequest tracks a plan approval request state.
type PlanRequest struct {
	From    string `json:"from"`
	Plan    string `json:"plan"`
	Status  string `json:"status"` // pending, approved, rejected
}

// ShutdownTracker tracks shutdown request/response handshakes.
type ShutdownTracker struct {
	mu       sync.Mutex
	requests map[string]*ShutdownRequest
}

func NewShutdownTracker() *ShutdownTracker {
	return &ShutdownTracker{requests: make(map[string]*ShutdownRequest)}
}

// Request sends a shutdown request to a teammate.
func (t *ShutdownTracker) Request(sender string, target string, bus *MessageBus) string {
	reqID := uuid.New().String()[:8]
	t.mu.Lock()
	t.requests[reqID] = &ShutdownRequest{Target: target, Status: "pending"}
	t.mu.Unlock()

	bus.Send(sender, target, "Please shut down.", "shutdown_request", map[string]interface{}{
		"request_id": reqID,
	})
	return fmt.Sprintf("Shutdown request %s sent to '%s'", reqID, target)
}

// HandleResponse processes a shutdown response.
func (t *ShutdownTracker) HandleResponse(reqID string, approve bool) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	req, ok := t.requests[reqID]
	if !ok {
		return fmt.Sprintf("Error: Unknown shutdown request_id '%s'", reqID)
	}
	if approve {
		req.Status = "approved"
	} else {
		req.Status = "rejected"
	}
	return fmt.Sprintf("Shutdown request %s: %s", reqID, req.Status)
}

// Check returns the status of a shutdown request.
func (t *ShutdownTracker) Check(reqID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	req, ok := t.requests[reqID]
	if !ok {
		return fmt.Sprintf("Unknown: %s", reqID)
	}
	return fmt.Sprintf("[%s] target=%s", req.Status, req.Target)
}

// PlanTracker tracks plan approval request/response handshakes.
type PlanTracker struct {
	mu       sync.Mutex
	requests map[string]*PlanRequest
}

func NewPlanTracker() *PlanTracker {
	return &PlanTracker{requests: make(map[string]*PlanRequest)}
}

// Request creates a plan approval request.
func (t *PlanTracker) Request(from string, plan string) string {
	reqID := uuid.New().String()[:8]
	t.mu.Lock()
	t.requests[reqID] = &PlanRequest{From: from, Plan: plan, Status: "pending"}
	t.mu.Unlock()
	return reqID
}

// Get returns a plan request by ID.
func (t *PlanTracker) Get(reqID string) *PlanRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.requests[reqID]
}

// UpdateStatus updates the status of a plan request.
func (t *PlanTracker) UpdateStatus(reqID string, status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if req, ok := t.requests[reqID]; ok {
		req.Status = status
	}
}

// ProtocolToolDefinitions returns shutdown and plan approval tool schemas.
func ProtocolToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "shutdown_request",
			Description: "Request a teammate to shut down.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"teammate": map[string]interface{}{"type": "string"},
				},
				"required": []string{"teammate"},
			},
		},
		{
			Name:        "plan_approval",
			Description: "Approve or reject a teammate's plan.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"request_id": map[string]interface{}{"type": "string"},
					"approve":    map[string]interface{}{"type": "boolean"},
					"feedback":   map[string]interface{}{"type": "string"},
				},
				"required": []string{"request_id", "approve"},
			},
		},
	}
}

// Global protocol instances (s_full integration)
var (
	ShutdownRequests = NewShutdownTracker()
	PlanRequests     = NewPlanTracker()
)

// HandleShutdownRequest initiates a shutdown handshake.
func HandleShutdownRequest(teammate string, bus *MessageBus) string {
	return ShutdownRequests.Request("lead", teammate, bus)
}

// HandlePlanReview processes a plan approval response.
func HandlePlanReview(reqID string, approve bool, feedback string, bus *MessageBus) string {
	req := PlanRequests.Get(reqID)
	if req == nil {
		return fmt.Sprintf("Error: Unknown plan request_id '%s'", reqID)
	}
	status := "approved"
	if !approve {
		status = "rejected"
	}
	PlanRequests.UpdateStatus(reqID, status)
	bus.Send("lead", req.From, feedback, "plan_approval_response", map[string]interface{}{
		"request_id": reqID,
		"approve":    approve,
		"feedback":   feedback,
	})
	return fmt.Sprintf("Plan %s for '%s'", status, req.From)
}

var _ = time.Now // reference to suppress "imported but not used" if not needed
