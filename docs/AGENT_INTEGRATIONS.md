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
