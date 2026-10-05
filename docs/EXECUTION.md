# Execution defaults

Every task is carried out with five settings:

| Setting | Values | Default |
| --- | --- | --- |
| **Agent** | an installed coding agent (`claude-code`, `codex`) | the first one that works |
| **Model** | a model of that agent, or **Agent default** | Agent default |
| **Reasoning** | one of that agent's reasoning/effort levels, or **Agent default** | Agent default |
| **Interaction** | *Ask me when needed*, *Work autonomously*, *Work autonomously — stop if blocked* | Ask me when needed |
| **Priority** | Low, Normal, High | Normal |

You do not have to set any of them: left alone, the agent chooses its own model and reasoning, and nothing is
passed to it.

## The hierarchy

```
global defaults  →  project defaults  →  task  →  (one run)
```

Each level stores only what it sets. The **first level that sets a field wins, so a task override always wins**,
then the project's, then the global default, then the built-in default. In the app, every picker says what leaving
it alone means: "Same as the project (Claude Code)".

- **Global defaults:** Settings → Defaults for every task.
- **Project defaults:** the project's Settings tab.
- **Task:** when you add a task (Options), when you edit it, or when a health finding creates one.
- **One run:** "Change for this run" when you start an agent. It affects that run only and changes nothing stored.

A run keeps a copy of what it started with (agent, model, reasoning, interaction): changing defaults or editing the
task changes the *next* run, never one that is working, and a session resumed after a restart is resumed with the
model and reasoning it started with.

### Models belong to an agent

A model name means nothing to another agent, so **choosing a model or reasoning level also fixes the agent at that
level**, and a level's model and reasoning only apply when its own agent is the one that wins. A task that switches
from Claude Code to Codex is never handed the project's `opus`. "Agent default" is a real choice, not an absence:
a task can ask for the agent's own default even when its project names a model.

## Models and reasoning levels come from the agent

Werkbord does not carry a list of model names:

- **Codex** reports its own models, with the reasoning levels each supports, through its app-server
  (`model/list`), so the list is whatever your installed Codex says it is, and follows its updates.
- **Claude Code** cannot list models; Werkbord offers its aliases (`sonnet`, `opus`, `haiku`, which always mean
  the latest of that family) and reads the `--effort` levels from the CLI's own help.
- Either way, **a name that is not listed can be typed in** ("Other…"); the agent has the last word. A name must
  look like a model (letters, digits and `._:/[]+@-`, not starting with `-`), because it is passed on a command line.
- You can replace the lists in `config.json` (`agents.<id>.models`, `agents.<id>.reasoning`), and `agents.<id>.model`
  is what "Agent default" means on this computer. If an agent cannot say, the lists are just "Agent default".
- A reasoning level the agent does not have is refused when you save it and when a run starts, with the ones it
  does have, rather than starting an agent that would fail on it.

## Where it is stored

Global defaults and the first-run record are in the `settings` table; a project's defaults in `projects.execution`;
a task's overrides in `tasks.execution` (JSON, so a new setting is not a migration); a run's model and reasoning on
the run. See migration `0008_execution_config.sql`. The old per-task interaction policy became the task's
`interaction` override when it was not the default; tasks that never chose one inherit, which until you change the
global default means "Ask me when needed", exactly as before.

## API

`GET /api/settings`, `PUT /api/settings/execution`, `PUT /api/projects/{pid}/execution`, `GET /api/agents`,
`GET /api/agents/{id}/options`; tasks take and return `execution`; `POST …/tasks/{id}/runs` takes optional
`agentId`, `model`, `reasoning` and `policy` for that run alone.
