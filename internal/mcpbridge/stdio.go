// Package mcpbridge provides the product-neutral stdio-to-HTTP MCP client used
// by both Werkbord commands. It forwards the remote server's tools, resources
// and prompts without adding credentials to protocol messages.
package mcpbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunStdio connects an external MCP host's stdio session to an authenticated
// Streamable HTTP endpoint. Only the remote endpoint receives the bearer token.
func RunStdio(ctx context.Context, endpoint, token string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("MCP endpoint must be an absolute HTTP or HTTPS URL without user info or a fragment")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("MCP grant credential is required (set WERKBORD_MCP_TOKEN or pass --token)")
	}
	httpClient := &http.Client{
		Transport: bearerTransport{token: token, base: http.DefaultTransport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "werkbord-stdio-bridge", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return fmt.Errorf("connect to Werkbord MCP endpoint: %w", err)
	}
	defer session.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "werkbord-stdio-bridge", Version: "1"}, &mcp.ServerOptions{
		Instructions: "This stdio bridge forwards only the tools, resources and prompts supplied by the configured Werkbord endpoint.",
	})
	for tool, err := range session.Tools(ctx, &mcp.ListToolsParams{}) {
		if err != nil {
			return fmt.Errorf("discover Werkbord MCP tools: %w", err)
		}
		remoteTool := tool
		server.AddTool(remoteTool, func(callCtx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return session.CallTool(callCtx, &mcp.CallToolParams{Name: remoteTool.Name, Arguments: req.Params.Arguments})
		})
	}
	for resource, err := range session.Resources(ctx, &mcp.ListResourcesParams{}) {
		if err != nil {
			return fmt.Errorf("discover Werkbord MCP resources: %w", err)
		}
		remoteResource := resource
		server.AddResource(remoteResource, func(readCtx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return session.ReadResource(readCtx, &mcp.ReadResourceParams{URI: req.Params.URI})
		})
	}
	for prompt, err := range session.Prompts(ctx, &mcp.ListPromptsParams{}) {
		if err != nil {
			return fmt.Errorf("discover Werkbord MCP prompts: %w", err)
		}
		remotePrompt := prompt
		server.AddPrompt(remotePrompt, func(promptCtx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return session.GetPrompt(promptCtx, &mcp.GetPromptParams{Name: remotePrompt.Name, Arguments: req.Params.Arguments})
		})
	}
	return server.Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header = req.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(copy)
}
