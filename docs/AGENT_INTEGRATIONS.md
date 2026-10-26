# CLI agents and automatic model selection

Werkbord uses installed CLIs and their own authentication. It does not install an
agent or an adapter, collect provider credentials, or execute a hosted IDE session.
Settings → Agents contains the full catalogue, installation links, billing declarations,
model discovery and automatic-selection inclusion. Elsewhere, controls are collapsing
selects; mobile activity shows installed agents and agents with active sessions.

## Catalogue

The original Claude Code, Codex, Gemini CLI and OpenCode integrations remain. The
catalogue adds Cursor, GitHub Copilot, Cline, Kilo, Amp, Auggie, Factory Droid, Goose,
Kimi CLI, Qwen Code, Mistral Vibe, Junie, Qoder, Pi, Google Antigravity, Devin,
Codebuddy Code, Snowflake Cortex Code, Grok Build, MiniMax Code and Kiro CLI: 25 in total.

The reviewed entry points are in `internal/agentcatalog/agents.json`. The evidence is
[the official ACP registry](https://agentclientprotocol.com/get-started/registry),
[its machine-readable index](https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json)
and [Kiro's V3 ACP migration guide](https://kiro.dev/docs/cli/v3/acp-migration/),
reviewed on 24 October 2026. npm executable names were checked against package manifests.
Amp and Pi require separately installed community ACP adapters; installation and
sign-in remain explicit user actions. A CLI on a different path can be configured
with `agents.<id>.command` in `config.json`. No command is downloaded at runtime.

These entries support **coding runs over ACP**. The original four also have Salie
providers. Additional Salie providers require a separately verified way to disable
execution tools and isolate conversation state; an ACP coding entry alone does not
prove that. Settings labels this limitation rather than running an unrestricted
coding CLI as Salie. Vendor binaries, authentication plans, operating systems and
protocol versions can still impose availability restrictions.

The controller probes version availability. On model inspection, new ACP entries
open a bounded, private protocol session with no prompt and no filesystem or terminal
capabilities. Model-specific reasoning controls are probed without prompts for up to 64 reported models per session; unprobed models do not inherit another model’s reasoning controls. Reported controls are cached; failed discovery
leaves Agent default available for manual use. A requested control the CLI does not
advertise fails before the first task prompt. Setup instructions link to each vendor
or adapter; installing an executable alone does not prove authentication.

## Four objectives

- **Lowest cost**: choose sufficient capability for the task, preferring included
  subscription usage, then owner-supplied model price estimates.
- **Max reasoning**: choose a suitable model and its highest reported reasoning level.
  Models without known reasoning controls are ineligible for this mode.
- **Highest intelligence**: prioritise the strongest suitable model and deep reasoning.
- **Manual**: retain the user's exact provider, model and reasoning choices.

New unconfigured work uses Lowest cost. Existing explicit agent/model choices stay
manual. Modes inherit through controller → project → task → run; an explicit mode
replaces lower manual model pins. Workflows carry the same objective into their tasks.
Authorised Team dispatches retain their reviewed concrete runtime choices. Resuming a
coding session reuses its recorded provider/model/reasoning; it is not reclassified.

Selection happens before the first Salie message or coding run starts and remains
fixed. Salie stores the objective and decision across controller restarts. Run records
store the concrete model and reasoning, with a system event explaining automatic
selection. Nothing silently switches a provider mid-conversation or retries a task
prompt after it might already have executed.

## Classifier and local fallback

A small, isolated CLI request assesses the task's complexity and preferred model
among eligible candidates. It receives bounded task text and candidate metadata, no
repository contents, board tools or execution permissions. The classifier itself
obeys the enabled-agent and metered-access policy. Timeout (15 seconds), malformed
output, unavailable classifier or invalid assessment uses deterministic local rules.
The decision reports `classifier` or `local_rules`; Salie and run history expose it.

Eligibility and objectives are enforced locally. Model IDs must come from the
candidate list. An AI preference cannot enable an excluded agent, authorise API
spending, invent a price or supply an unreported reasoning level. Fallback estimates
complexity from task text and model capability from model-family tiers; Settings
permits capability overrides. These are estimates, not benchmark claims. An unknown
provider default or `auto` alias cannot be compared as a known model.

## Spending and availability

Only agents enabled in Settings participate. Existing four integrations begin enabled
for upgrade compatibility; added agents require opt-in. Billing has four states:
CLI detection, included subscription (declared by the owner), metered, and excluded.
Unknown access is ineligible rather than assumed free. A detected API-key override
remains metered even if the owner declared a subscription.

Metered candidates require **Allow metered API fallback** and both input/output USD
prices per million tokens. Prices are explicit owner estimates, not fabricated live
quotes. Ranking compares equal input/output quantities after the capability requirement;
actual context, output, caching, provider multipliers, taxes and subscription allowance
are not predicted. Unknown remaining allowance is never represented as a measured zero
or balance. Lowest cost prefers included usage; Highest intelligence prioritises
capability before the cost tie-break. No subscription-exhaustion retry occurs after
sending a task prompt.

Availability is filtered before selection, using the existing runner, repository,
authorisation, capacity and option checks. Another eligible option can win when one
is already known unavailable. A failure after task delivery is reported normally;
there is no blind redispatch. Manual choices are never substituted automatically.

## Verification boundaries

Protocol discovery, permissions, model dispatch, mode inheritance, billing exclusion,
classifier fallback, session persistence and the browser dropdown flow are covered by
local tests and disposable fixtures. The catalogue is documentation-verified; this
does not claim a paid-account end-to-end run against all 25 vendors. Existing trust
boundary tests remain in force, and Team does not run models or hold credentials.

## One description of each agent, two ways to run it

A coding agent is used in two ways, and the code keeps them apart on purpose. An **adapter** (`internal/agent`) keeps a
long-lived interactive session in a task's worktree. A **provider** (`internal/assistant/provider`) answers one isolated
Salie turn in an empty directory with its tools off. Their runtimes, their guards and their errors differ.

What they share is the **facts about the command line**, and those live in one place (`internal/agentkit`): is the command
there, which version is it, is anyone signed in and how, which reasoning levels does it take, what do we tell the person
when it is not usable. `agentkit.Prober` asks the CLI once and remembers the answer for 30 seconds; the controller gives
the same Prober to every adapter and provider, so `claude --version`, `claude auth status` and `claude --help` each run
once, not once per family. Agents known by name (Gemini CLI, OpenCode) are described once in `agentcatalog.Builtin`,
and the reviewed catalogue entries in `agents.json`.

To add an agent that speaks ACP, add its entry to `agents.json`: nothing else is needed. To add one with its own protocol,
write the adapter (and, if Salie may use it, the provider) and ask `agentkit` for the facts; do not run `--version` or a
sign-in status command from either. `internal/archtest` fails if an adapter or a provider does.

## Salie Sessions Phase 1: direct CLI availability

Settings → Agents separately stores direct CLI opt-in and an optional owner declaration that the CLI
has been signed in on this device. The existing detection result remains authoritative when it reports
signed out. Unknown authentication is not relabelled as a verified connection. Neither setting enables
automatic model selection, installs tools, collects credentials or authorises execution.

The catalogue exposes an independent native interactive capability with device, authentication,
support and authorisation fields. Phase 1 reports support/authorisation false because the native
interactive terminal is deferred to Phase 3. Salie's + menu requires every eligibility gate plus a real
frontend launcher; it therefore shows no coding launch entries yet, only the Salie conversation action
and a Settings link. Settings and detection are fetched each time the menu opens, so future eligible
launchers will not depend on restarting the application. The complete catalogue stays in Settings.

Verification: `sessions.test.ts` covers five isolated streams, per-tab settings/drafts/confirmations,
close/reopen, legacy migration and reload restoration. `connected-agents.test.ts` checks every eligibility
gate. `selection_interactive_test.go` locks down Phase 1's absent execution capability, and
`TestDirectCLIPreferencesPersistIndependentlyOfAutomaticSelection` covers settings persistence.
`node scripts/browser-salie-sessions.cjs` exercises the real controller API and assistant engine with a
scripted provider and disposable SQLite, including five running sessions and desktop/mobile captures.
