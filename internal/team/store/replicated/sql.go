package replicated

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Stmt is one SQL statement with its arguments, as a use case ran it on the local copy and as the cluster
// is asked to run it. The arguments are the values SQLite stores: nil, int64, string and []byte. (Team's
// schema has no REAL column, so a float is an error here rather than a number that might not survive being
// written as JSON and read back.)
type Stmt struct {
	SQL  string
	Args []any
}

// maxExactInt is the largest integer a JSON number carries without loss in every reader.
const maxExactInt = 1 << 53

// normalize converts arguments the way database/sql does before a driver sees them.
func normalize(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		v, err := driver.DefaultParameterConverter.ConvertValue(a)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i+1, err)
		}
		switch x := v.(type) {
		case nil, string, []byte:
			out[i] = x
		case int64:
			if x > maxExactInt || x < -maxExactInt {
				return nil, fmt.Errorf("argument %d: %d is too large to be sent to the cluster exactly", i+1, x)
			}
			out[i] = x
		case bool:
			if x {
				out[i] = int64(1)
			} else {
				out[i] = int64(0)
			}
		case float64:
			if x == math.Trunc(x) && math.Abs(x) < maxExactInt {
				out[i] = int64(x)
				break
			}
			return nil, fmt.Errorf("argument %d: a floating-point value (%v): Team's schema has none", i+1, x)
		default:
			return nil, fmt.Errorf("argument %d: a %T cannot be sent to the cluster", i+1, v)
		}
	}
	return out, nil
}

// wire is the statement in the form rqlite takes: [sql, arg, ...], a BLOB as an array of bytes.
func (s Stmt) wire() []any {
	out := make([]any, 0, len(s.Args)+1)
	out = append(out, s.SQL)
	for _, a := range s.Args {
		if b, ok := a.([]byte); ok {
			ints := make([]int, len(b))
			for i, c := range b {
				ints[i] = int(c)
			}
			out = append(out, ints)
			continue
		}
		out = append(out, a)
	}
	return out
}

// ---- the form a batch is kept in, in the cluster's log of writes ----

type loggedStmt struct {
	Q string            `json:"q"`
	A []json.RawMessage `json:"a,omitempty"`
}

func encodeStmts(stmts []Stmt) (string, error) {
	out := make([]loggedStmt, len(stmts))
	for i, s := range stmts {
		ls := loggedStmt{Q: s.SQL}
		for _, a := range s.Args {
			var raw []byte
			var err error
			switch x := a.(type) {
			case nil:
				raw = []byte("null")
			case []byte:
				raw, err = json.Marshal(map[string]string{"b": base64.StdEncoding.EncodeToString(x)})
			default:
				raw, err = json.Marshal(x)
			}
			if err != nil {
				return "", err
			}
			ls.A = append(ls.A, raw)
		}
		out[i] = ls
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func decodeStmts(text string) ([]Stmt, error) {
	var in []loggedStmt
	if err := json.Unmarshal([]byte(text), &in); err != nil {
		return nil, err
	}
	out := make([]Stmt, len(in))
	for i, ls := range in {
		s := Stmt{SQL: ls.Q}
		for _, raw := range ls.A {
			a, err := decodeArg(raw)
			if err != nil {
				return nil, err
			}
			s.Args = append(s.Args, a)
		}
		out[i] = s
	}
	return out, nil
}

func decodeArg(raw json.RawMessage) (any, error) {
	t := strings.TrimSpace(string(raw))
	switch {
	case t == "null":
		return nil, nil
	case strings.HasPrefix(t, `"`):
		var s string
		return s, json.Unmarshal(raw, &s)
	case strings.HasPrefix(t, "{"):
		var m struct {
			B string `json:"b"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(m.B)
	default:
		var n json.Number
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, err
		}
		i, err := n.Int64()
		if err != nil {
			return nil, errors.New("a logged argument is a number that is not an integer")
		}
		return i, nil
	}
}

// ---- telling a write from a read ----

// firstWord is the SQL's first keyword, upper-cased, skipping whitespace and comments.
func firstWord(sql string) string {
	i := 0
	for i < len(sql) {
		switch {
		case sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r' || sql[i] == '(':
			i++
		case strings.HasPrefix(sql[i:], "--"):
			j := strings.IndexByte(sql[i:], '\n')
			if j < 0 {
				return ""
			}
			i += j + 1
		case strings.HasPrefix(sql[i:], "/*"):
			j := strings.Index(sql[i+2:], "*/")
			if j < 0 {
				return ""
			}
			i += j + 4
		default:
			j := i
			for j < len(sql) && (sql[j] >= 'a' && sql[j] <= 'z' || sql[j] >= 'A' && sql[j] <= 'Z') {
				j++
			}
			return strings.ToUpper(sql[i:j])
		}
	}
	return ""
}

type kind int

const (
	kindRead kind = iota
	kindWrite
	kindForbidden
)

// classify says whether a statement a use case ran reads, writes, or is something an Update may not do
// (it would change the connection and not the data, or end the transaction the cluster's request is).
func classify(sql string) kind {
	switch w := firstWord(sql); w {
	case "SELECT", "VALUES", "EXPLAIN":
		return kindRead
	case "WITH":
		// A common table expression may end in a write.
		up := strings.ToUpper(sql)
		for _, dml := range []string{"INSERT", "UPDATE", "DELETE", "REPLACE"} {
			if strings.Contains(up, dml) {
				return kindWrite
			}
		}
		return kindRead
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "CREATE", "DROP", "ALTER":
		return kindWrite
	default:
		return kindForbidden
	}
}
