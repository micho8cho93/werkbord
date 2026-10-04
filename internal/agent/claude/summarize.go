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

// approvalRequest says what the agent wants to do, as a short question and
// the detail the user needs to judge it. The question is the same for every
// tool of a kind; the specifics (the command, the plan, the change) are context.
func approvalRequest(tool string, input map[string]any, workDir string) (prompt, detail string) {
	str := func(k string) string { v, _ := input[k].(string); return strings.TrimSpace(v) }
	switch tool {
	case "Bash":
		detail = "$ " + clip(str("command"), 1500)
		if d := str("description"); d != "" {
			detail += "\n\n" + clip(d, 300)
		}
		return "Run this command?", detail
	case "Write":
		return "Write to " + relativeTo(workDir, str("file_path")) + "?", clip(str("content"), 1500)
	case "Edit":
		return "Edit " + relativeTo(workDir, str("file_path")) + "?", changeDetail(str("old_string"), str("new_string"))
	case "MultiEdit":
		detail = ""
		if edits, ok := input["edits"].([]any); ok && len(edits) > 0 {
			if first, ok := edits[0].(map[string]any); ok {
				o, _ := first["old_string"].(string)
				n, _ := first["new_string"].(string)
				detail = changeDetail(strings.TrimSpace(o), strings.TrimSpace(n))
			}
			if len(edits) > 1 {
				detail += fmt.Sprintf("\n… and %d more changes", len(edits)-1)
			}
		}
		return "Edit " + relativeTo(workDir, str("file_path")) + "?", detail
	case "NotebookEdit":
		return "Edit notebook " + relativeTo(workDir, firstNonEmpty(str("notebook_path"), str("file_path"))) + "?", clip(str("new_source"), 1500)
	case "ExitPlanMode":
		return "Approve this plan?", clip(str("plan"), 8000)
	}
	return fmt.Sprintf("Allow %s?", tool), summarizeTool(tool, input, workDir)
}

// changeDetail shows an edit as the text removed and the text added.
func changeDetail(oldText, newText string) string {
	var b strings.Builder
	if oldText != "" {
		b.WriteString("Replace:\n" + clip(oldText, 700) + "\n\n")
	}
	if newText != "" {
		b.WriteString("With:\n" + clip(newText, 700))
	}
	return strings.TrimSpace(b.String())
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
