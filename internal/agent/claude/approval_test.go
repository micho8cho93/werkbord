package claude

import (
	"strings"
	"testing"
)

// The question is short and the same for every use of a tool; what the user
// needs in order to judge it is the context.
func TestApprovalRequestSplitsTheQuestionFromTheDetail(t *testing.T) {
	const dir = "/work/tree"
	for _, tc := range []struct {
		name         string
		tool         string
		input        map[string]any
		prompt       string
		contextHas   []string
		contextLacks []string
	}{
		{"a command", "Bash", map[string]any{"command": "rm -rf build", "description": "clean up"},
			"Run this command?", []string{"$ rm -rf build", "clean up"}, nil},
		{"writing a file", "Write", map[string]any{"file_path": dir + "/a/b.go", "content": "package a\n"},
			"Write to a/b.go?", []string{"package a"}, []string{dir}},
		{"editing a file", "Edit", map[string]any{"file_path": dir + "/main.go", "old_string": "foo()", "new_string": "bar()"},
			"Edit main.go?", []string{"Replace:\nfoo()", "With:\nbar()"}, nil},
		{"several edits", "MultiEdit", map[string]any{"file_path": dir + "/m.go", "edits": []any{
			map[string]any{"old_string": "a", "new_string": "b"}, map[string]any{"old_string": "c", "new_string": "d"}, map[string]any{"old_string": "e", "new_string": "f"}}},
			"Edit m.go?", []string{"Replace:\na", "With:\nb", "2 more changes"}, nil},
		{"a plan", "ExitPlanMode", map[string]any{"plan": "1. Do the thing\n2. Test it"},
			"Approve this plan?", []string{"1. Do the thing", "2. Test it"}, nil},
		{"a tool nobody special-cased", "WebFetch", map[string]any{"url": "https://example.com/x"},
			"Allow WebFetch?", []string{"https://example.com/x"}, nil},
		{"an edit with nothing to show", "Edit", map[string]any{"file_path": dir + "/e.go"},
			"Edit e.go?", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompt, detail := approvalRequest(tc.tool, tc.input, dir)
			if prompt != tc.prompt {
				t.Errorf("prompt = %q, want %q", prompt, tc.prompt)
			}
			for _, want := range tc.contextHas {
				if !strings.Contains(detail, want) {
					t.Errorf("context %q lacks %q", detail, want)
				}
			}
			for _, bad := range tc.contextLacks {
				if strings.Contains(detail, bad) {
					t.Errorf("context %q should not contain %q", detail, bad)
				}
			}
			if strings.Contains(prompt, "\n") {
				t.Errorf("the question is one line: %q", prompt)
			}
		})
	}
}

func TestApprovalContextIsBounded(t *testing.T) {
	_, detail := approvalRequest("Bash", map[string]any{"command": strings.Repeat("x", 100_000)}, "/w")
	if n := len([]rune(detail)); n > 2000 {
		t.Fatalf("a command of any length gives %d runes of context", n)
	}
	_, plan := approvalRequest("ExitPlanMode", map[string]any{"plan": strings.Repeat("p", 100_000)}, "/w")
	if n := len([]rune(plan)); n > 9000 {
		t.Fatalf("plan context = %d runes", n)
	}
}
