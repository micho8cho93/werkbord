# MCP clients for Werkbord boards

Werkbord exposes two separate MCP endpoints. Individual runs its own grant-scoped server at `/mcp`; each Team workspace
runs a metadata-only server at its own `/mcp`. A credential for one endpoint cannot be used at the other. Neither server
starts a run, changes execution settings, reaches a member's computer, or accepts a proposal approval from an MCP client.

## Individual

Open **Settings → Connections → External MCP clients**. Create a client credential by selecting its projects and exact
operations. The credential is shown once. Revoke it from the same section at any time. Werkbord stores only its digest.

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

Team grants are separate from member and device credentials. A member can issue one through the authenticated Team API,
selecting only projects they can see and the Team metadata operations they want to delegate:

```http
POST /api/team/v1/mcp/grants
Authorization: Bearer <member token>
Content-Type: application/json

{"name":"planning agent","projects":["<project id>"],"operations":["list_projects","list_tickets","get_ticket","get_schedule","create_ticket","update_ticket"]}
```

The response contains the new `wbtm_…` credential once. The member can list their grants with `GET /api/team/v1/mcp/grants`
and revoke one with `DELETE /api/team/v1/mcp/grants/<grant id>`. A grant cannot add to the member's role or project
membership: Team rechecks both on every request. The Team MCP service only reads and updates stored project and task
metadata; Team still coordinates and never executes the work.

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
