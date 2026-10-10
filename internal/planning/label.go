package planning

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on labels. They are generous: a label is a few words, and a task carries a handful.
const (
	MaxLabelNameLen        = 40
	MaxLabelDescriptionLen = 200
	// MaxLabelsPerTask is how many labels one task or ticket may carry.
	MaxLabelsPerTask = 20
	// MaxLabels is how many labels one workspace may define.
	MaxLabels = 500
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// CleanLabelName trims a label's name and folds runs of white space into one space. Names are
// whatever the person wants: nothing about them is predefined.
func CleanLabelName(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	switch {
	case s == "":
		return "", invalid("label name is required")
	case !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxLabelNameLen:
		return "", invalid("label name is longer than %d characters", MaxLabelNameLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", invalid("label name contains a control character")
		}
	}
	return s, nil
}

// LabelKey is what makes two names the same label: "Design" and "design " are one name.
func LabelKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// CleanLabelColor accepts #rgb or #rrggbb (any case) and returns #rrggbb in lower case. A colour is
// only ever one of these, so it can be put in a style attribute without further escaping.
func CleanLabelColor(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == 4 && s[0] == '#' {
		s = "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]})
	}
	if len(s) != 7 || s[0] != '#' {
		return "", invalid("label colour %q is not a #rrggbb colour", s)
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", invalid("label colour %q is not a #rrggbb colour", s)
		}
	}
	return s, nil
}

// CleanLabelDescription trims an optional one-line explanation of what a label is for.
func CleanLabelDescription(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxLabelDescriptionLen {
		return "", invalid("label description is longer than %d characters", MaxLabelDescriptionLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", invalid("label description contains a control character")
		}
	}
	return s, nil
}

// CleanLabelIDs checks the set of labels put on one task: no more than MaxLabelsPerTask, each ID
// once. It returns the IDs in the order given with repeats dropped; whether each exists is the
// caller's to check. A nil set is an empty one.
func CleanLabelIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 100 {
			return nil, invalid("a label ID is not usable")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > MaxLabelsPerTask {
		return nil, invalid("a task can carry at most %d labels", MaxLabelsPerTask)
	}
	return out, nil
}
