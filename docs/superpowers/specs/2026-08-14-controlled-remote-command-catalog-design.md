# Controlled Remote Command Catalog — Agent Foundation

## Requirement

`docs/specs/controlled-remote-command-catalog.md` requires the agent to grow from a single hardcoded
action into a small, explicit, auditable catalog. Its "Decisão pendente" section blocks every new
action until the first additional command, its operational justification and its idempotency are
decided. This design covers only the part of the specification that does not depend on that decision:
the mechanism that a future action will plug into.

`KILL_ANYDESK` stays the only registered action.

## Behavior

### Compiled catalog

- Actions are declared in Go code, never read from the queue or from configuration.
- Each action declares: name, argument schema, idempotency, maximum accepted time-to-live and whether
  it is enabled by default on a machine.
- Dispatch happens through the catalog entry. There is no path from received text to a process name,
  a shell or an interpreter.
- An action that is not in the catalog is rejected with `action is not allowed`.

### Per-machine authorization

- `allowedActions` in `config.json` is an install-time allowlist for that machine.
- The effective set is the intersection of the compiled catalog and `allowedActions`.
- When `allowedActions` is absent, only actions marked as enabled by default apply. A future action is
  therefore inert until an operator enables it on a specific machine.
- An action that exists in the catalog but is not enabled on the machine is rejected with
  `action is not authorized on this machine`, which is distinct from an unknown action.

### Argument schema per action

- Each action parses its own arguments. Unknown keys, wrong types and non-object arguments are
  rejected before anything executes.
- `KILL_ANYDESK` keeps exactly one optional boolean argument, `reopenAnyDesk`.

### Expiration ceiling

- The envelope keeps `requestedAt`/`expiresAt` and the existing UTC and expiry checks.
- Additionally, `expiresAt - requestedAt` may not exceed the action's declared maximum. A producer
  cannot mint a command that stays valid for hours.

### Deduplication by `commandId`

- The agent keeps a bounded, time-limited ledger of executed `commandId` values.
- A repeated `commandId` is not executed again. It publishes a `DUPLICATE` result so the caller still
  gets a correlated answer, and it is audited.
- The ledger is bounded by both entry age and entry count so a long-running agent cannot grow without
  limit.

### Audit

- Every decision emits one audit event: accepted-and-executed, failed, duplicate or rejected.
- An audit event carries `commandId`, `correlationId`, `target`, `action`, `requestedBy`, `status`,
  `reason`, and timestamps. It never carries arguments, raw payloads or configuration values.
- Audit and result lists are trimmed to a fixed retention so Redis usage stays bounded.

## Scope

Go agent only. The Python MVP under `agent/` stays frozen on the single-action path documented in the
README; the specification's prerequisite is the Go V1 agent.

## Out of scope, and why

- **New actions.** Blocked by the specification's pending decision.
- **Redis Streams, consumer groups and `XACK`.** The specification requires this before the first
  non-idempotent action. The n8n Redis node exposes `push`/`pop` list operations and no `XADD`, so the
  producer side of a Streams migration needs its own decision — a different producer node, a small
  publisher service, or accepting a non-n8n producer. Migrating the consumer alone would leave the
  workflow unable to enqueue. Recorded as the next blocking step instead of half-implemented.
- **Envelope signing and Redis ACLs per agent.** Deployment-side work, tracked separately.

## Testing

- Rejection tests for: unknown action, action not authorized on the machine, non-object arguments,
  unknown argument key, wrong argument type, expiry beyond the action ceiling.
- Ledger tests for: duplicate detection, expiry of old entries, and the size bound.
- Audit tests proving the event carries the required fields and omits arguments and raw payloads.
