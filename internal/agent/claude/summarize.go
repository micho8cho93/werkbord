package claude

import (
	"fmt"
	"sort"
	"strings"
)

// summarizeTool turns a tool call into one line for the activity feed:
// "Edit internal/api/server.go", "Bash: go test ./...". Paths inside the
// working directory are shown relative to it.
func summarizeTool(name string, input map[string]any, workDir string) string {
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := input[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	path := func() string { return relativeTo(workDir, str("file_path", "notebook_path", "path")) }
	var detail string
	switch name {
	case "Bash":
		detail = firstLine(str("command"))
	case "Read", "Edit", "MultiEdit", "Write", "NotebookEdit":
		detail = path()
	case "Grep", "Glob":
		detail = str("pattern")
		if p := relativeTo(workDir, str("path")); p != "" {
			detail += " in " + p
		}
	case "WebFetch":
		detail = str("url")
	case "WebSearch":
		detail = str("query")
	case "Task", "Agent":
		detail = str("description")
	case "TodoWrite":
		return "Updating the to-do list"
	default:
		detail = firstLine(str("description", "command", "file_path", "path", "query", "url", "prompt"))
		if detail == "" {
			detail = firstKey(input)
		}
	}
	if detail == "" {
		return name
	}
	if name == "Bash" {
		return "Bash: " + clip(detail, 300)
	}
	return name + " " + clip(detail, 300)
}

func firstKey(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return firstLine(v)
		}
	}
	return ""
}

func relativeTo(dir, path string) string {
	if path == "" || dir == "" {
		return path
	}
	if rel, ok := strings.CutPrefix(path, strings.TrimSuffix(dir, "/")+"/"); ok {
		return rel
	}
	return path
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// approvalPrompt is what the user is asked when the agent wants a tool.
func approvalPrompt(tool string, input map[string]any, workDir string) string {
	str := func(k string) string { v, _ := input[k].(string); return strings.TrimSpace(v) }
	switch tool {
	case "Bash":
		p := "Run this command?\n$ " + clip(str("command"), 1500)
		if d := str("description"); d != "" {
			p += "\n" + clip(d, 300)
		}
		return p
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		verb := map[string]string{"Write": "Write to", "NotebookEdit": "Edit notebook"}[tool]
		if verb == "" {
			verb = "Edit"
		}
		return verb + " " + relativeTo(workDir, firstNonEmpty(str("file_path"), str("notebook_path"))) + "?"
	case "ExitPlanMode":
		return "Approve this plan?\n" + clip(str("plan"), 4000)
	}
	return fmt.Sprintf("Allow %s?\n%s", tool, summarizeTool(tool, input, workDir))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
