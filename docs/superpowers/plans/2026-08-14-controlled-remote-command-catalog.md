# Controlled Remote Command Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Go agent the catalog mechanism required by `docs/specs/controlled-remote-command-catalog.md` without adding any action beyond `KILL_ANYDESK`, which stays blocked on the specification's pending decision.

**Architecture:** A compiled action registry declares schema, idempotency, lifetime ceiling and default enablement per action. `validateCommand` resolves the envelope against the registry and the machine allowlist, a bounded ledger answers duplicate `commandId` values before any handler runs, and every decision emits a payload-free audit event next to the structured result.

**Tech Stack:** Go 1.24, Python 3, GitHub Actions.

## Global Constraints

- `KILL_ANYDESK` remains the only catalog entry.
- No `os/exec` call may take a string that came from the queue.
- A new catalog entry must be inert until a machine lists it in `allowedActions`.
- Audit events carry no arguments, no raw envelope and no configuration value.
- The n8n envelope shape stays compatible: existing producers keep working.

---

### Task 1: Compiled action catalog

**Files:**
- Create: `agent-go/catalog.go`, `agent-go/catalog_test.go`
- Modify: `agent-go/main.go`, `agent-go/agent_test.go`

**Interfaces:**
- Produces: `actionSpec`, `actionSet`, `catalog`, `defaultActions()`, `configuredActions([]string)`
- Produces: `validateCommand(string, string, actionSet, time.Time) (command, actionSpec, error)`

- [x] **Step 1: Write the failing tests**

Rejection tests for an unknown action, an action not enabled on the machine, a non-object argument
object, an unknown argument key, a wrong argument type, a lifetime beyond the action ceiling and an
oversized `requestedBy`.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./...`
Expected: FAIL because the catalog does not exist.

- [x] **Step 3: Write minimal implementation**

Add the registry with the single `KILL_ANYDESK` entry, dispatch `validateCommand` through it, and
resolve `allowedActions` in `loadConfig`.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

### Task 2: Deduplication ledger

**Files:**
- Create: `agent-go/ledger.go`, `agent-go/ledger_test.go`

**Interfaces:**
- Produces: `newCommandLedger(time.Duration, int)`, `(*commandLedger).admit(string, time.Time) bool`

- [x] **Step 1: Write the failing tests**

Admit an unseen id, refuse a repeat, forget an entry older than the window, stay within the size bound.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./...`
Expected: FAIL because `newCommandLedger` does not exist.

- [x] **Step 3: Write minimal implementation**

Mutex-guarded map plus insertion order, pruned by age on every admission and capped by count.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

### Task 3: Structured audit

**Files:**
- Create: `agent-go/audit.go`, `agent-go/audit_test.go`

**Interfaces:**
- Produces: `auditEvent`, `commandAudit(...)`, `rejectionAudit(...)`

- [x] **Step 1: Write the failing tests**

Assert the executed-command event carries requester, target, action, status and both timestamps;
assert the marshalled event contains no argument; assert a rejection event bounds its reason and
copies nothing from the rejected payload.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./...`
Expected: FAIL because `commandAudit` does not exist.

- [x] **Step 3: Write minimal implementation**

Add the event type, both constructors and the reason bound.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS.

### Task 4: Consume loop

**Files:**
- Modify: `agent-go/main.go`, `agent-go/main_test.go`

**Interfaces:**
- Produces: `resultPayload(...)`, `deadLetterPayload(...)`, `publish(...)`, `report(...)`, `reportRejection(...)`

- [x] **Step 1: Write the failing tests**

Payload builders for success and duplicate results and for the dead letter, plus `loadConfig` tests
covering the default allowlist, an unknown allowed action and an empty list.

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./...`
Expected: FAIL because `resultPayload` does not exist.

- [x] **Step 3: Write minimal implementation**

Dispatch through `spec.execute`, gate on the ledger, publish result and audit with bounded retention,
and turn an execution failure into a `FAILED` result instead of a transport teardown. Create the
ledger in `main` so it survives reconnections.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./...` and `GOOS=windows GOARCH=amd64 go build ./...`
Expected: PASS.

### Task 5: Documentation

**Files:**
- Create: `docs/specs/adicionar-nova-action.md`
- Modify: `docs/specs/controlled-remote-command-catalog.md`, `README.md`, `agent/config.example.json`

- [x] **Step 1: Record the process for a new action**

Checklist with the mandatory spec, the short threat model, the catalog entry rules, the rejection
tests and the transport prerequisite for a non-idempotent action.

- [x] **Step 2: Record what is implemented and what still blocks**

Update the specification status, and state that the Redis Streams migration is blocked on the
producer side because the n8n Redis node has no `XADD`.

- [x] **Step 3: Document the operator-facing surface**

`allowedActions`, the audit list and the result statuses in the README and the config example.
