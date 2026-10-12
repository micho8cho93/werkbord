// Package mcpcontract contains the product-neutral MCP transport contract used
// by Individual and Team. It owns only protocol framing and grant scope shape;
// each product supplies its own authentication, operations and authorization.
package mcpcontract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	MaxProjects   = 100
	MaxOperations = 64
	MaxNameRunes  = 80
	maxBodyBytes  = 1 << 20
)

// Principal is the non-secret scope attached to one authenticated MCP client.
// Product handlers must still apply their normal permission and project checks.
type Principal struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Projects   []string `json:"projects"`
	Operations []string `json:"operations"`
}

// Allows reports whether this grant names the operation.
func (p Principal) Allows(operation string) bool {
	for _, candidate := range p.Operations {
		if candidate == operation {
			return true
		}
	}
	return false
}

// CanSee reports whether this grant names the project.
func (p Principal) CanSee(project string) bool {
	for _, candidate := range p.Projects {
		if candidate == project {
			return true
		}
	}
	return false
}

// Validate checks and canonicalizes a user-created grant scope.
func (p *Principal) Validate() error {
	if p == nil {
		return errors.New("grant is required")
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || utf8.RuneCountInString(p.Name) > MaxNameRunes || strings.ContainsAny(p.Name, "\r\n\x00") {
		return fmt.Errorf("name must contain 1 to %d characters", MaxNameRunes)
	}
	if len(p.Projects) == 0 || len(p.Projects) > MaxProjects {
		return fmt.Errorf("choose between 1 and %d projects", MaxProjects)
	}
	if len(p.Operations) == 0 || len(p.Operations) > MaxOperations {
		return fmt.Errorf("choose between 1 and %d operations", MaxOperations)
	}
	if err := canonicalStrings("project", p.Projects, 128); err != nil {
		return err
	}
	if err := canonicalStrings("operation", p.Operations, 64); err != nil {
		return err
	}
	for _, op := range p.Operations {
		for i, r := range op {
			if (i == 0 && (r < 'a' || r > 'z')) || (i > 0 && !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_')) {
				return fmt.Errorf("operation %q has an invalid name", op)
			}
		}
	}
	sort.Strings(p.Projects)
	sort.Strings(p.Operations)
	return nil
}

func canonicalStrings(kind string, values []string, max int) error {
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > max || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("%s ids must contain 1 to %d characters", kind, max)
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = true
	}
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return nil
}

// Operation is an MCP-visible operation. InputSchema is JSON Schema and must
// be enforced by Call, because the low-level SDK API does not validate it.
type Operation struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type principalKey struct{}

// WithPrincipal puts the current request's principal in its context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// PrincipalFromContext returns the principal authenticated for this request.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// Authenticate resolves a request credential to a product-owned grant.
type Authenticate func(*http.Request) (Principal, bool)

// AuthenticateContext can attach a product-owned actor to the request context
// while returning the common MCP scope.
type AuthenticateContext func(*http.Request) (Principal, context.Context, bool)

// ListOperations returns only operations the product is willing to expose to
// this principal. Call must recheck current scope and product permissions.
type ListOperations func(Principal) []Operation

// Call performs a tool operation through the product's own application layer.
type Call func(context.Context, Principal, string, json.RawMessage) (any, error)

// StreamableHandler builds an authenticated Streamable HTTP endpoint. The
// authenticate callback runs on every HTTP request, including requests in an
// established MCP session, so revocation takes effect immediately.
func StreamableHandler(authenticate Authenticate, list ListOperations, call Call) http.Handler {
	return StreamableHandlerWithContext(func(r *http.Request) (Principal, context.Context, bool) {
		principal, ok := authenticate(r)
		return principal, nil, ok
	}, list, call)
}

// StreamableHandlerWithContext is StreamableHandler with a product context
// hook for requests that also need a current domain actor.
func StreamableHandlerWithContext(authenticate AuthenticateContext, list ListOperations, call Call) http.Handler {
	stream := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			return nil
		}
		server := mcp.NewServer(&mcp.Implementation{Name: "werkbord", Version: "1"}, &mcp.ServerOptions{
			Instructions: "This MCP server exposes only the board operations granted to this client. Mutations may create a pending proposal and never approve it.",
		})
		for _, operation := range list(principal) {
			if operation.Name == "" || len(operation.InputSchema) == 0 || !json.Valid(operation.InputSchema) {
				continue
			}
			op := operation
			server.AddTool(&mcp.Tool{Name: op.Name, Title: op.Title, Description: op.Description, InputSchema: json.RawMessage(op.InputSchema)}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				current, ok := PrincipalFromContext(ctx)
				if !ok || !current.Allows(op.Name) {
					return toolError("permission_denied", "this client is not permitted to use that operation"), nil
				}
				if len(req.Params.Arguments) > maxBodyBytes {
					return toolError("too_large", "tool arguments are too large"), nil
				}
				data, err := call(ctx, current, op.Name, req.Params.Arguments)
				if err != nil {
					return toolError("operation_failed", err.Error()), nil
				}
				encoded, err := json.Marshal(data)
				if err != nil || len(encoded) > maxBodyBytes {
					return toolError("too_large", "tool result is too large to return"), nil
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: data}, nil
			})
		}
		return server
	}, &mcp.StreamableHTTPOptions{JSONResponse: true, MaxRequestBodyBytes: maxBodyBytes})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authenticate == nil || list == nil || call == nil {
			http.Error(w, "MCP is unavailable", http.StatusServiceUnavailable)
			return
		}
		principal, extra, ok := authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="werkbord-mcp"`)
			http.Error(w, "valid MCP client credentials are required", http.StatusUnauthorized)
			return
		}
		ctx := r.Context()
		if extra != nil {
			ctx = extra
		}
		stream.ServeHTTP(w, r.WithContext(WithPrincipal(ctx, principal)))
	})
}

func toolError(code, message string) *mcp.CallToolResult {
	data := map[string]string{"code": code, "error": message}
	encoded, _ := json.Marshal(data)
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: data}
}
