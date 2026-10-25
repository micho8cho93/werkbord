package agentcatalog

// NativeTerminal is an explicit reviewed capability, independent of ACP and chat adapters.
// Both entry points start their native interactive UI with no arguments. Never reuse ACP args.
func NativeTerminal(id string) bool { return NativeCommand(id) != "" }
func NativeCommand(id string) string {
	switch id {
	case "claude-code":
		return "claude"
	case "codex":
		return "codex"
	}
	return ""
}
