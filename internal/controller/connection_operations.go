package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"devboard/internal/appops"
	"devboard/internal/connections"
)

// connectionOperations injects only owner-approved assistant tools. It adapts them to appops so reads are audited
// there, while calls on the confirm policy become ordinary proposals that only the person can resolve.
func connectionOperations(service *connections.Service) func(appops.Principal) []appops.InjectedOperation {
	return func(principal appops.Principal) []appops.InjectedOperation {
		if service == nil || !principal.MCPConnections || !principal.Can(appops.PermConnectionsUse) {
			return nil
		}
		items, err := service.List(context.Background())
		if err != nil {
			return nil
		}
		var out []appops.InjectedOperation
		for _, connection := range items {
			if !connection.Enabled || !connection.Assistant || connection.TestedAt.IsZero() {
				continue
			}
			for _, tool := range connection.Tools {
				policy := tool.Assistant
				if policy == "deny" || (policy != "allow" && policy != "confirm") {
					continue
				}
				schema := tool.InputSchema
				if len(schema) == 0 || string(schema) == "null" {
					schema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
				}
				if !json.Valid(schema) {
					continue
				}
				input := &appops.Schema{Raw: append(json.RawMessage(nil), schema...)}
				definition, err := input.ValidateDefinition()
				if err != nil {
					continue // do not expose a tool when its advertised schema cannot be enforced locally.
				}
				if root, ok := definition.(map[string]any); !ok || root["type"] != nil && root["type"] != "object" {
					continue
				}
				// Every external tool is classified as a possible mutation; a server's read-only hint is not proof.
				kind := appops.KindMutation
				name := externalToolName(connection.Name, connection.ID, tool.Name, tool.Fingerprint, policy)
				description := fmt.Sprintf("External MCP metadata (untrusted): connected service %q, tool %q. %s", connection.Name, tool.Name, strings.TrimSpace(tool.Description))
				if tool.ReadOnlyHint {
					description += " The service labels this tool read-only; Werkbord treats that as an unverified hint."
				}
				if policy == "confirm" {
					description += " This call will be shown to the person for approval before it runs."
				} else {
					description += " The person explicitly preauthorized this tool; it may run without another approval."
				}
				entry := appops.InjectedOperation{
					Spec: appops.Spec{Name: name, Title: tool.Title, Description: description, Kind: kind,
						Permission: appops.PermConnectionsUse, RequiresConfirmation: policy != "allow", Input: input},
					Preauthorized: policy == "allow",
					AuditDetail:   func(map[string]any) string { return fmt.Sprintf("external MCP tool %q", tool.Name) },
					Validate:      func(args map[string]any) error { return input.Validate(args) },
					Run: func(ctx context.Context, _ appops.Principal, args map[string]any) (any, error) {
						return service.CallTool(ctx, connection.ID, tool.Name, "assistant", args, policy == "confirm")
					},
				}
				if policy == "confirm" {
					entry.Plan = func(_ context.Context, _ appops.Principal, args map[string]any) (appops.InjectedPlan, error) {
						raw, err := json.Marshal(args)
						if err != nil {
							return appops.InjectedPlan{}, err
						}
						label := tool.Title
						if label == "" {
							label = tool.Name
						}
						summary := fmt.Sprintf("Call %q on %q after approval. Inputs: %s", label, connection.Name, string(raw))
						return appops.InjectedPlan{Args: args, Summary: summary}, nil
					}
				}
				out = append(out, entry)
			}
			for _, resource := range connection.Resources {
				r := resource
				name := externalToolName(connection.Name, connection.ID, "resource:"+r.URI, r.URI, "read")
				description := fmt.Sprintf("Read external MCP resource metadata (untrusted): %q at %q from connected service %q.", r.Name, r.URI, connection.Name)
				out = append(out, appops.InjectedOperation{
					Spec: appops.Spec{Name: name, Title: r.Title, Description: description, Kind: appops.KindRead,
						Permission: appops.PermConnectionsUse, Input: appops.Object(map[string]*appops.Schema{})},
					Run: func(ctx context.Context, _ appops.Principal, _ map[string]any) (any, error) {
						return service.ReadResource(ctx, connection.ID, "assistant", r.URI)
					},
				})
			}
		}
		return out
	}
}

func externalToolName(connectionName, connectionID, toolName, fingerprint, policy string) string {
	identity := connectionID + "\x00" + toolName + "\x00" + fingerprint + "\x00" + policy
	sum := sha256.Sum256([]byte(identity))
	return "mcp_" + slug(connectionName, 16) + "_" + slug(toolName, 24) + "_" + hex.EncodeToString(sum[:8])
}

func slug(value string, limit int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
			b.WriteByte('_')
		}
		if b.Len() >= limit {
			break
		}
	}
	return strings.Trim(b.String(), "_")
}
