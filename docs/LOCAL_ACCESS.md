# Local access: a narrow way in for other programs on this computer

Werkbord's own access token can do everything its owner can: register repositories, change settings, push and merge, pair
runners. Some programs on your computer need far less than that: to hand Werkbord a task, to see what is running, to answer an
agent's question or to stop a run. Those are given a **local access token** of their own, instead of yours.

```
werkbord access list              which programs have access, and when each last used it
werkbord access revoke <id>       take a program's access away, at once
```

A program is given access from its own side: it asks the controller, signing in with the controller's token, which you
have allowed it to read. The controller returns a token for that program once and keeps only a hash of it
(`<data dir>/local-access.json`, readable by you alone). A program that connects again under the same name replaces its
old token. At most sixteen programs may have access.

## What a program with access can do, and what it cannot

It is accepted on a short, reviewed list of routes (`internal/api/localaccess.go`; a test fails if the list changes without
the change being read):

| | |
| --- | --- |
| **See** | the projects, a project's tasks, runs and questions, the runners, the Control Center, and its own access |
| **Hand over a task** | `POST /api/projects/{id}/tasks`, with a title, a description, where it came from and the branches. Not the task's execution settings or its schedule: those are yours |
| **Start, stop, answer** | start a task's run **with the task's own configuration** (it cannot name an agent, a model, a policy, extra instructions or a runner), stop a run, answer an agent's question |

It **cannot**: register or change a repository, change any setting, touch Git or GitHub, pair or revoke a runner, read or
rotate the controller's token, give access to another program or see who has it, or reach the Werkbord on your private
network. Every other route answers it `403`, whatever the route would have done.

What a run may do is still decided where it always was: by the task's and the project's execution settings, by the agent's
own permission prompts, which come to you, and by the interaction policy you chose. A program that starts a run does not
choose how it runs.

## Where it is accepted

Only from this computer (a loopback address), and only on the controller's loopback listener. The private network that
reaches your phone never accepts one. If you turn the controller's token off (`requireToken: false`), there is nothing to narrow
and no access tokens are given out.

## For programs that use it

`POST /api/local-access` `{"name": "My program"}`, with the controller's token, returns `{"id", "name", "createdAt", "token"}`
once. Use the token as `Authorization: Bearer wba_…`. `GET /api/local-access/self` says whether it still works. A program should
keep its token as carefully as it would keep any credential, and the owner can revoke it whenever they like.
