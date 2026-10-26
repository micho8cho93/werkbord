// Package agentkit is what the coding-agent adapters (internal/agent) and the assistant's providers
// (internal/assistant/provider) both need to know about the same command-line agents: how to find one, which version it
// is, whether it is signed in, which reasoning levels it takes and what models it offers by name.
//
// The two families stay separate where they differ: an adapter keeps a long-lived interactive session in a worktree, a
// provider answers one isolated turn. They used to carry their own copies of everything above, so a fix to how Claude
// Code is detected had to be made twice and each family asked the same CLI the same questions again. Now there is one
// place that asks, one answer that both read, and one place the messages are written.
//
// agentkit starts a process only to ask a question (`--version`, `auth status`, `--help`), never to do work, and it
// never reads or passes a credential.
package agentkit
