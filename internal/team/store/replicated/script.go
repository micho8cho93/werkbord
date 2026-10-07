package replicated

import (
	"strings"
)

// splitScript splits a migration, which may hold many statements, into the statements the cluster is
// sent one by one (rqlite executes one statement per entry). It understands what Team's migrations use:
// quoted strings and identifiers, comments (dropped), and the BEGIN ... END body of a trigger, whose
// semicolons do not end the statement and inside which a CASE ... END does not end it either.
func splitScript(script string) []string {
	var out []string
	var cur strings.Builder
	var words []string // the statement's words so far, upper-cased
	trigger, depth := false, 0
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
		words = words[:0]
		trigger, depth = false, 0
	}
	endWord := func(w string) {
		if w == "" {
			return
		}
		w = strings.ToUpper(w)
		words = append(words, w)
		if len(words) <= 3 && w == "TRIGGER" && words[0] == "CREATE" {
			trigger = true
		}
		if trigger {
			switch w {
			case "BEGIN":
				depth++
			case "CASE":
				depth++
			case "END":
				depth--
			}
		}
	}
	var word strings.Builder
	isWordChar := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	i := 0
	for i < len(script) {
		c := script[i]
		if !isWordChar(c) && word.Len() > 0 {
			endWord(word.String())
			word.Reset()
		}
		switch {
		case isWordChar(c):
			word.WriteByte(c)
			cur.WriteByte(c)
			i++
		case c == '-' && i+1 < len(script) && script[i+1] == '-':
			for i < len(script) && script[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(script) && script[i+1] == '*':
			j := strings.Index(script[i+2:], "*/")
			if j < 0 {
				i = len(script)
			} else {
				i += j + 4
			}
			cur.WriteByte(' ')
		case c == '\'' || c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			cur.WriteByte(c)
			i++
			for i < len(script) {
				cur.WriteByte(script[i])
				if script[i] == closer {
					if closer != ']' && i+1 < len(script) && script[i+1] == closer { // a doubled quote is a quote
						cur.WriteByte(script[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == ';':
			if trigger && depth > 0 {
				cur.WriteByte(c)
				i++
				continue
			}
			i++
			flush()
		default:
			cur.WriteByte(c)
			i++
		}
	}
	if word.Len() > 0 {
		endWord(word.String())
	}
	flush()
	return out
}
