# MCP clients for Werkbord boards

Werkbord exposes two separate MCP endpoints. Individual runs its own grant-scoped server at `/mcp`; each Team workspace
runs a metadata-only server at its own `/mcp`. A credential for one endpoint cannot be used at the other. Neither server
starts a run, changes execution settings, reaches a member's computer, or accepts a proposal approval from an MCP client.

## Individual

Open **Settings → Connections → MCP services → Connect a client to Werkbord**. Create a client credential by selecting its projects and exact
operations. The credential is shown once. Revoke it from the same section at any time. Werkbord stores only its digest.

After creating a credential, use **Copy connection settings**. Werkbord generates a portable `mcpServers` entry with
the address and authentication filled in. Choose the stdio option for clients that require a local process. The generated
configuration includes the credential: store it in the client's private configuration, then hide it in Werkbord. Nothing
is installed in another app automatically. Clients differ in their import UI; URL-based screens need the endpoint and
an `Authorization` header with `Bearer <credential>`.

If the client runs on another computer, replace the address with a reachable private controller address ending in `/mcp`.
`localhost` always means the client's own computer. A paired phone's relay address is not a general-purpose MCP endpoint.
Reconnect the client and ask it to list projects to check the connection.

For the reverse direction, **Connect Werkbord to a service** accepts a single-server `mcpServers` JSON configuration.
Import fills in the form without connecting or executing anything. Review it, save, test and discover, then enable the
connection and its assistant/coding access. Configurations with environment secrets or unsupported settings require
manual entry so settings are not silently dropped. **Help → Use an MCP service** and **Connect a client to Werkbord** explain the two directions.

The Streamable HTTP endpoint is the controller's address followed by `/mcp`, for example:

```text
http://127.0.0.1:7420/mcp
```

An MCP host that supports Streamable HTTP can connect directly and send the grant as `Authorization: Bearer …`. For a
host that supports stdio, configure the local bridge and put the grant in that process's environment:

```json
{
  "mcpServers": {
    "werkbord": {
      "command": "werkbord",
      "args": ["mcp", "--url", "http://127.0.0.1:7420/mcp"],
      "env": { "WERKBORD_MCP_TOKEN": "<credential from Settings>" }
    }
  }
}
```

Board reads are limited to the selected projects. A task change creates a proposal in Werkbord; review and approval happen
in the app. The client can inspect the status of its own proposals, but it cannot approve them. Revoking a credential stops
new requests immediately and keeps its pending proposal available for the person to decide.

## Team

In the Team console, open **Settings → MCP clients** to issue or revoke a client credential. Select only projects you can
see and the operations the agent needs. The authenticated Team API exposes the same grant controls:

```http
POST /api/team/v1/mcp/grants
Authorization: Bearer <member token>
Content-Type: application/json

{"name":"planning agent","projects":["<project id>"],"operations":["list_projects","list_tickets","get_ticket","get_schedule","propose_task_plan","list_pending_proposals","get_proposal_status"]}
```

The response contains the new `wbtm_…` credential once. The member can list their grants with `GET /api/team/v1/mcp/grants`
and revoke one with `DELETE /api/team/v1/mcp/grants/<grant id>`. A grant cannot add to the member's role or project
membership: Team rechecks both on every request. A grant that includes `create_ticket` or `update_ticket` can make those
single-task changes directly. `propose_task_plan` submits one project's new or version-checked task batch for review; it
does not apply the plan. Open **Plans** in the Team console to edit the task list, inspect timeline warnings, and approve
or decline it. Approval rechecks current permissions, task versions, source references, and dependencies, then applies
the batch in one transaction. Temporary IDs (`tmp_…`) let new tasks depend on each other. Reuse an idempotency key only
when retrying the same request. The client can list and check only its own proposals; a person makes the decision.

Team stores board metadata and never executes the work.

The Team endpoint is the workspace host address followed by `/mcp`. A stdio host can use the Team bridge:

```json
{
  "mcpServers": {
    "werkbord-team": {
      "command": "werkbord-team",
      "args": ["mcp", "--url", "http://127.0.0.1:7430/mcp"],
      "env": { "WERKBORD_TEAM_MCP_TOKEN": "<credential from Team>" }
    }
  }
}
```

Use the host's address on the customer-owned private network when the MCP client runs elsewhere. If Team is behind a TLS
proxy, use its HTTPS address. No Werkbord Slack bot or public relay is included; Slack interaction requires a separate
agent that can reach the private endpoint.
