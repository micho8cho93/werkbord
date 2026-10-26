package daemon

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	textunicode "golang.org/x/text/encoding/unicode"
)

// Definition is what an installed controller service says about itself: which
// executable it runs, and where it keeps its data.
type Definition struct {
	// Manager is the operating system service manager that owns the definition.
	Manager string
	// Binary is the executable the service starts, as `werkbord setup` wrote it.
	Binary string
	// DataDir is the data directory it was given (WERKBORD_DATA_DIR in its
	// environment), or "" when it was left to the default.
	DataDir string
}

var (
	plistProgram   = regexp.MustCompile(`(?s)<key>ProgramArguments</key>\s*<array>\s*<string>(.*?)</string>`)
	plistEnvKeys   = []string{"WERKBORD_DATA_DIR"}
	windowsDataDir = regexp.MustCompile(`(?i)set\s+"WERKBORD_DATA_DIR=([^"]*)"`)
	xmlEncoding    = regexp.MustCompile(`(?i)(<\?xml[^>]*encoding\s*=\s*["'])[^"']+(["'])`)
)

// Describe reads the controller service's definition, without starting, stopping
// or changing anything. A program that did not start from a terminal (the desktop
// app, which the Finder starts with almost no environment) uses it to join the
// installation that is already there instead of making a second one next to it: the
// service says which executable it runs and which data directory it was given, and
// `werkbord setup` was the one that decided both.
//
// It reads the current user's launchd, systemd or Task Scheduler definition. On
// systems without an installed login service, ok is false.
func Describe(ctx context.Context, o Options) (Definition, bool) {
	m := Detect(ctx, o)
	return describeDefinition(ctx, m)
}

func describeDefinition(ctx context.Context, m Manager) (Definition, bool) {
	var def Definition
	var ok bool
	switch v := m.(type) {
	case *Launchd:
		body, err := os.ReadFile(v.plistPath())
		if err == nil {
			def, ok = parsePlist(string(body))
		}
	case *Systemd:
		body, err := os.ReadFile(v.unitPath())
		if err == nil {
			def, ok = parseSystemdUnit(string(body))
		}
	case *Schtasks:
		body, found := v.queryXML(ctx)
		if found {
			def, ok = parseScheduledTask(body)
		}
	default:
		return Definition{}, false
	}
	if ok {
		def.Manager = m.Name()
	}
	return def, ok
}

func parsePlist(body string) (Definition, bool) {
	var d Definition
	if m := plistProgram.FindStringSubmatch(body); m != nil {
		d.Binary = xmlUnescape(strings.TrimSpace(m[1]))
	}
	for _, key := range plistEnvKeys {
		re := regexp.MustCompile(`(?s)<key>` + key + `</key>\s*<string>(.*?)</string>`)
		if m := re.FindStringSubmatch(body); m != nil {
			d.DataDir = xmlUnescape(strings.TrimSpace(m[1]))
			break
		}
	}
	return d, d.Binary != ""
}

func parseSystemdUnit(body string) (Definition, bool) {
	var def Definition
	for _, line := range strings.Split(body, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		words, err := systemdWords(value)
		if err != nil || len(words) == 0 {
			continue
		}
		switch key {
		case "ExecStart":
			if def.Binary == "" {
				def.Binary = systemdUnescape(words[0])
			}
		case "Environment":
			pair := strings.SplitN(systemdUnescape(words[0]), "=", 2)
			if len(pair) == 2 && pair[0] == "WERKBORD_DATA_DIR" {
				def.DataDir = pair[1]
			}
		}
	}
	return def, def.Binary != ""
}

// systemdWords reverses the quoting emitted by quoteUnit for the executable and
// environment assignments in Werkbord's own unit files.
func systemdWords(s string) ([]string, error) {
	var words []string
	var word strings.Builder
	quoted, escaped, active := false, false, false
	flush := func() {
		if active {
			words = append(words, word.String())
			word.Reset()
			active = false
		}
	}
	for _, r := range s {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
			active = true
		case r == '\\':
			escaped = true
			active = true
		case r == '"':
			quoted = !quoted
			active = true
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			word.WriteRune(r)
			active = true
		}
	}
	if escaped || quoted {
		return nil, errors.New("invalid systemd quoting")
	}
	flush()
	return words, nil
}

func systemdUnescape(s string) string {
	return strings.NewReplacer("%%", "%", "$$", "$").Replace(s)
}

func parseScheduledTask(body string) (Definition, bool) {
	decoded, err := decodeTaskXML(body)
	if err != nil {
		return Definition{}, false
	}
	var task struct {
		Actions struct {
			Exec struct {
				Command   string `xml:"Command"`
				Arguments string `xml:"Arguments"`
			} `xml:"Exec"`
		} `xml:"Actions"`
	}
	if err := xml.Unmarshal(decoded, &task); err != nil || !strings.EqualFold(filepathBase(task.Actions.Exec.Command), "cmd.exe") && !strings.EqualFold(filepathBase(task.Actions.Exec.Command), "cmd") {
		return Definition{}, false
	}
	args := strings.TrimSpace(task.Actions.Exec.Arguments)
	if len(args) >= 2 && (args[0] == '"' && args[len(args)-1] == '"') {
		args = args[1 : len(args)-1]
	}
	if i := strings.Index(strings.ToLower(args), "/c"); i >= 0 {
		args = strings.TrimSpace(args[i+2:])
	}
	def := Definition{}
	if m := windowsDataDir.FindStringSubmatch(args); m != nil {
		def.DataDir = m[1]
	}
	if i := strings.LastIndex(args, " && "); i >= 0 {
		args = strings.TrimSpace(args[i+4:])
	}
	if strings.HasPrefix(args, "\"") {
		if end := strings.Index(args[1:], "\""); end >= 0 {
			def.Binary = args[1 : end+1]
		}
	} else if fields := strings.Fields(args); len(fields) > 0 {
		def.Binary = fields[0]
	}
	return def, def.Binary != ""
}

func decodeTaskXML(body string) ([]byte, error) {
	raw := []byte(body)
	var decoded []byte
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		var err error
		decoded, err = textunicode.UTF16(textunicode.LittleEndian, textunicode.UseBOM).NewDecoder().Bytes(raw)
		if err != nil {
			return nil, err
		}
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		var err error
		decoded, err = textunicode.UTF16(textunicode.BigEndian, textunicode.UseBOM).NewDecoder().Bytes(raw)
		if err != nil {
			return nil, err
		}
	case len(raw) > 1 && raw[0] == '<' && raw[1] == 0:
		var err error
		decoded, err = textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM).NewDecoder().Bytes(raw)
		if err != nil {
			return nil, err
		}
	case len(raw) > 1 && raw[0] == 0 && raw[1] == '<':
		var err error
		decoded, err = textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM).NewDecoder().Bytes(raw)
		if err != nil {
			return nil, err
		}
	default:
		decoded = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	}
	if !utf8.Valid(decoded) {
		return nil, errors.New("Task Scheduler returned XML in an unsupported encoding")
	}
	text := xmlEncoding.ReplaceAll(decoded, []byte(`${1}UTF-8${2}`))
	return bytes.TrimPrefix(text, []byte{0xef, 0xbb, 0xbf}), nil
}

func filepathBase(s string) string {
	if i := strings.LastIndexAny(s, `/\\`); i >= 0 {
		return s[i+1:]
	}
	return s
}

var xmlUnescaper = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&#39;", "'", "&#34;", `"`, "&amp;", "&")

func xmlUnescape(s string) string { return xmlUnescaper.Replace(s) }
