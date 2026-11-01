# Documentation

These guides describe the current application, retained API contracts and operating requirements. Completed implementation
plans, dated audits and superseded mockups live in Git history. Accepted architecture decisions remain under `adr/` because
their reasoning still applies; their original context is historical, and current guides take precedence for operation.
The [retired release-title history](archive/RELEASE_HISTORY.md) is retained under the repository's versioning policy.

## Product and architecture

- [Product](../PRODUCT.md), [design](../DESIGN.md) and [agent instructions](../AGENTS.md)
- [Structure and trust boundaries](STRUCTURE.md), [architecture](ARCHITECTURE.md) and [versioning](VERSIONING.md)
- [Free and Premium](PLANS.md), [desktop navigation](UNIFIED_NAVIGATION.md) and [navigation history](NAVIGATION.md)
- [Team architecture decision](adr/0001-team-production-architecture.md), [customer-owned network](adr/0002-customer-owned-network.md)
  and [replicated storage](adr/0003-replicated-workspace-storage.md)

## Features and contracts

- [Work queues and plans](WORK.md), [planning](PLANNING.md), [Team initiatives](INITIATIVES.md) and [catch-up](CATCH_UP.md)
- [Salie](ASSISTANT.md), [agent sessions](AGENT_SESSIONS.md), [native terminals](NATIVE_TERMINALS.md),
  [agent integrations](AGENT_INTEGRATIONS.md) and [MCP](MCP.md)
- [Execution](EXECUTION.md), [coordination](EXECUTION_COORDINATION.md), [recovery](EXECUTION_RECOVERY.md),
  [orchestration](ORCHESTRATION.md) and [runners](RUNNERS.md)
- [Git](GIT.md), [repository health](HEALTH.md), [recommendations](RECOMMENDATIONS.md), [Product Map](PRODUCT_MAP.md)
  and [analytics](ANALYTICS.md)
- [Personal memory](MEMORY.md), [Team memory](TEAM_MEMORY.md), [memory sync](MEMORY_SYNC.md),
  [integration](INTEGRATION.md), [local access](LOCAL_ACCESS.md) and [retained project-transfer contract](unified-platform/PROJECT_MOVES.md)
- [Onboarding](ONBOARDING.md), [mobile interface](MOBILE.md), [phone access](PHONE.md), [relay](RELAY.md),
  [multiple devices](MULTI_DEVICE.md) and [multi-device acceptance](MULTI_DEVICE_ACCEPTANCE.md)
- [Legibility](LEGIBILITY.md) and [motion](MOTION.md)

## Installation and operations

- [Controller installation](INSTALL.md), [desktop](DESKTOP.md) and [desktop releases](DESKTOP_RELEASE.md)
- [Team](TEAM.md), [Team installation](TEAM_INSTALL.md), [desktop integration](TEAM_DESKTOP.md),
  [network](TEAM_NETWORK.md), [storage](TEAM_STORAGE.md), [security](TEAM_SECURITY.md) and [phone security](TEAM_PHONE_SECURITY.md)
- [Backups](TEAM_BACKUP_RECOVERY.md), [host replacement](TEAM_HOST_REPLACEMENT.md) and [disaster recovery](TEAM_DISASTER_RECOVERY.md)
- [Premium operator console](PREMIUM_OPERATIONS.md) and [local demo data](DEMO_DATA.md)
- [Beta distribution](BETA_READINESS.md), [production acceptance](PRODUCTION_ACCEPTANCE.md),
  [Team security gate](TEAM_SECURITY_GATE.md), [functional acceptance](unified-platform/ACCEPTANCE.md)
  and the [acceptance ledger](RELEASE_ACCEPTANCE.json)
