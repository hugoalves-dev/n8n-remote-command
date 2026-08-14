package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func auditedCommand() command {
	return command{
		CommandID:     "8ddc0c10-253a-4e37-895f-c4b749e1c312",
		CorrelationID: "d840886f-3684-4180-b8b2-97c0402e6c69",
		Target:        "jcpc38",
		Action:        killAnyDeskAction,
		Args:          json.RawMessage(`{"reopenAnyDesk":true}`),
		RequestedBy:   "n8n",
		RequestedAt:   time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
		arguments:     killAnyDeskArgs{ReopenAnyDesk: true},
	}
}

func TestCommandAuditRecordsRequesterTargetActionAndResult(t *testing.T) {
	receivedAt := time.Date(2026, 8, 14, 10, 0, 1, 0, time.UTC)
	completedAt := receivedAt.Add(2 * time.Second)

	event := commandAudit(auditedCommand(), auditSucceeded, "", receivedAt, completedAt)

	if event.RequestedBy != "n8n" || event.Target != "jcpc38" || event.Action != killAnyDeskAction {
		t.Fatalf("commandAudit() = %+v, want the requester, target and action", event)
	}
	if event.Status != auditSucceeded || !event.CompletedAt.Equal(completedAt) || !event.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("commandAudit() = %+v, want the result and both timestamps", event)
	}
}

func TestCommandAuditNeverCarriesArguments(t *testing.T) {
	receivedAt := time.Date(2026, 8, 14, 10, 0, 1, 0, time.UTC)

	payload, err := json.Marshal(commandAudit(auditedCommand(), auditSucceeded, "", receivedAt, receivedAt))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if strings.Contains(string(payload), "reopenAnyDesk") || strings.Contains(string(payload), "args") {
		t.Fatalf("audit event leaked arguments: %s", payload)
	}
}

func TestRejectionAuditKeepsTheReasonBounded(t *testing.T) {
	receivedAt := time.Date(2026, 8, 14, 10, 0, 1, 0, time.UTC)

	event := rejectionAudit("jcpc38", strings.Repeat("a", maxAuditReasonLength*2), receivedAt)

	if event.Status != auditRejected {
		t.Fatalf("rejectionAudit() status = %q, want %q", event.Status, auditRejected)
	}
	if len(event.Reason) != maxAuditReasonLength {
		t.Fatalf("rejectionAudit() reason length = %d, want %d", len(event.Reason), maxAuditReasonLength)
	}
}

func TestRejectionAuditCarriesNoCommandFields(t *testing.T) {
	receivedAt := time.Date(2026, 8, 14, 10, 0, 1, 0, time.UTC)

	event := rejectionAudit("jcpc38", "action is not allowed", receivedAt)

	if event.CommandID != "" || event.Action != "" || event.RequestedBy != "" {
		t.Fatalf("rejectionAudit() = %+v, want no field taken from the rejected payload", event)
	}
}
