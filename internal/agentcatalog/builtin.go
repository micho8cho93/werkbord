package agentcatalog

// Builtin is an agent Werkbord knows by name and starts over ACP, with settings the generic catalog entries do not have.
// The coding-agent adapters and the assistant's providers for Gemini CLI and OpenCode both read it, so what the command is,
// how it is started and where its documentation lives are written once.
type Builtin struct {
	Entry
	// ModelFlag is the command-line flag that chooses a model, for an agent that takes it there.
	ModelFlag string
	// SessionOptions: the agent takes its model and reasoning choice as ACP session config options.
	SessionOptions bool
	// ListModels: the agent can list its models with `<command> models`.
	ListModels bool
}

// GeminiCLI is Google's Gemini CLI in its ACP mode.
func GeminiCLI() Builtin {
	return Builtin{
		Entry: Entry{
			ID: "gemini-cli", Name: "Gemini CLI", Command: "gemini", Args: []string{"--acp"},
			DocsURL: "https://geminicli.com/docs/cli/acp-mode/",
			Setup:   "Install Gemini CLI and complete sign-in from a terminal. Werkbord reuses its existing configuration:",
		},
		ModelFlag: "--model",
	}
}

// OpenCode is OpenCode in its ACP mode.
func OpenCode() Builtin {
	return Builtin{
		Entry: Entry{
			ID: "opencode", Name: "OpenCode", Command: "opencode", Args: []string{"--pure", "acp"},
			DocsURL: "https://opencode.ai/docs/acp/",
			Setup:   "Install OpenCode and run `opencode auth login` for a configured provider. Werkbord reuses its existing configuration:",
		},
		SessionOptions: true, ListModels: true,
	}
}
