// Package mcpplan defines the product-neutral request shape for a proposed
// batch of task changes. Product services resolve IDs and enforce their own
// permissions inside their transactions.
package mcpplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"devboard/internal/planning"
)

const (
	MaxChanges = 50
	MaxText    = 20000
	MaxSource  = 2000
)

// Plan is one idempotent, single-project batch. Temporary IDs let new tasks
// depend on one another before the server assigns their permanent IDs.
type Plan struct {
	ProjectID      string   `json:"projectId"`
	IdempotencyKey string   `json:"idempotencyKey"`
	Tasks          []Change `json:"tasks"`
}

// Change creates a task when TempID is set, or edits a task at Version when
// TaskID is set. Nil patch fields leave existing values alone.
type Change struct {
	TempID       string          `json:"tempId,omitempty"`
	TaskID       string          `json:"taskId,omitempty"`
	Version      int64           `json:"version,omitempty"`
	Title        *string         `json:"title,omitempty"`
	Description  *string         `json:"description,omitempty"`
	Requirements *string         `json:"requirements,omitempty"`
	Plan         *planning.Range `json:"plan,omitempty"`
	Dependencies *[]string       `json:"dependencies,omitempty"`
	SourceRef    *string         `json:"sourceRef,omitempty"`
}

// Preview contains timeline conflicts that a reviewer should see before the
// batch is approved. Errors are rejected by the product service.
type Preview struct {
	Warnings []planning.Warning `json:"warnings"`
}

// Applied maps temporary IDs to created task IDs and lists every write.
type Applied struct {
	Created []string          `json:"created"`
	Updated []string          `json:"updated"`
	TempIDs map[string]string `json:"tempIds"`
}

// Digest returns a stable content digest used to reject reuse of an
// idempotency key for a different plan.
func Digest(plan Plan) string {
	b, _ := json.Marshal(plan)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Clean validates and canonicalizes the portable parts of a task plan.
func Clean(in Plan) (Plan, error) {
	in.ProjectID = strings.TrimSpace(in.ProjectID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.ProjectID == "" || len(in.ProjectID) > 128 {
		return Plan{}, fmt.Errorf("projectId must contain 1 to 128 characters")
	}
	if in.IdempotencyKey == "" || len(in.IdempotencyKey) > 128 || strings.ContainsAny(in.IdempotencyKey, "\r\n\x00") {
		return Plan{}, fmt.Errorf("idempotencyKey must contain 1 to 128 characters")
	}
	if len(in.Tasks) == 0 || len(in.Tasks) > MaxChanges {
		return Plan{}, fmt.Errorf("a task plan must contain 1 to %d changes", MaxChanges)
	}
	temps, existing := map[string]bool{}, map[string]bool{}
	for i := range in.Tasks {
		c := &in.Tasks[i]
		c.TempID, c.TaskID = strings.TrimSpace(c.TempID), strings.TrimSpace(c.TaskID)
		switch {
		case c.TempID != "" && c.TaskID == "":
			if !strings.HasPrefix(c.TempID, "tmp_") || !validID(c.TempID) || temps[c.TempID] {
				return Plan{}, fmt.Errorf("task %d has an invalid or duplicate tempId", i+1)
			}
			if c.Version != 0 || c.Title == nil {
				return Plan{}, fmt.Errorf("new task %q needs a title and cannot have a version", c.TempID)
			}
			temps[c.TempID] = true
		case c.TempID == "" && c.TaskID != "":
			if !validID(c.TaskID) || existing[c.TaskID] || c.Version < 1 {
				return Plan{}, fmt.Errorf("task %d needs a unique taskId and a positive version", i+1)
			}
			existing[c.TaskID] = true
		default:
			return Plan{}, fmt.Errorf("task %d must have exactly one of tempId or taskId", i+1)
		}
		if c.Title != nil {
			value, err := cleanText(*c.Title, 200, "title")
			if err != nil || value == "" {
				return Plan{}, fmt.Errorf("task %d title is required and must be at most 200 characters", i+1)
			}
			c.Title = &value
		}
		for label, field := range map[string]**string{"description": &c.Description, "requirements": &c.Requirements} {
			if *field == nil {
				continue
			}
			value, err := cleanText(**field, MaxText, label)
			if err != nil {
				return Plan{}, err
			}
			*field = &value
		}
		if c.Plan != nil {
			clean, err := planning.CleanRange(*c.Plan)
			if err != nil {
				return Plan{}, err
			}
			c.Plan = &clean
		}
		if c.SourceRef != nil {
			value, err := cleanText(*c.SourceRef, MaxSource, "sourceRef")
			if err != nil {
				return Plan{}, err
			}
			c.SourceRef = &value
		}
		if c.Dependencies != nil {
			if len(*c.Dependencies) > 100 {
				return Plan{}, fmt.Errorf("task %d has more than 100 dependencies", i+1)
			}
			seen := map[string]bool{}
			clean := make([]string, 0, len(*c.Dependencies))
			for _, raw := range *c.Dependencies {
				dep := strings.TrimSpace(raw)
				if !validID(dep) || seen[dep] {
					return Plan{}, fmt.Errorf("task %d has an invalid or duplicate dependency", i+1)
				}
				seen[dep] = true
				clean = append(clean, dep)
			}
			c.Dependencies = &clean
		}
	}
	for i, c := range in.Tasks {
		if c.Dependencies == nil {
			continue
		}
		for _, dep := range *c.Dependencies {
			if strings.HasPrefix(dep, "tmp_") && !temps[dep] {
				return Plan{}, fmt.Errorf("task %d depends on unknown temporary task %q", i+1, dep)
			}
		}
	}
	return in, nil
}

func validID(s string) bool {
	if s == "" || len(s) > 128 || strings.ContainsAny(s, "\r\n\x00") {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func cleanText(s string, max int, field string) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || strings.ContainsRune(s, '\x00') {
		return "", fmt.Errorf("%s is too long or contains invalid text", field)
	}
	return s, nil
}
