package integration

import "time"

// ExecutionRequest is a task-bound local authorization, never a shell request.
// Fence binds external assignment/schedule context without naming a product.
type ExecutionRequest struct {
	NotBefore   time.Time `json:"notBefore,omitempty"`
	Deadline    time.Time `json:"deadline,omitempty"`
	ExecutionID string    `json:"executionId"`
	ProjectID   string    `json:"projectId"`
	TaskID      string    `json:"taskId"`
	Fence       string    `json:"fence"`
	RunnerID    string    `json:"runnerId"`
	AgentID     string    `json:"agentId"`
	Model       string    `json:"model"`
	Reasoning   string    `json:"reasoning"`
	Interaction string    `json:"interaction"`
}
type ExecutionPreview struct {
	Request       ExecutionRequest  `json:"request"`
	Digest        string            `json:"digest"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	RuntimePolicy map[string]string `json:"runtimePolicy"`
	StopBehavior  string            `json:"stopBehavior"`
}
type ExecutionApproval struct {
	Preview   ExecutionPreview `json:"preview"`
	ExpiresAt time.Time        `json:"expiresAt"`
	RunID     string           `json:"runId,omitempty"`
	Revoked   bool             `json:"revoked"`
}
type ExecutionDispatch struct {
	ExecutionID string `json:"executionId"`
	Fence       string `json:"fence"`
}
