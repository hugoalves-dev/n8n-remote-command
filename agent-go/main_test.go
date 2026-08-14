package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedisOptionsUsesConfiguredDatabase(t *testing.T) {
	var c config
	c.Redis.Database = 3

	options := redisOptions(c, "127.0.0.1:6379")

	if options.DB != 3 {
		t.Fatalf("expected Redis database 3, got %d", options.DB)
	}
}

func TestRedisOptionsDefaultsToDatabaseZero(t *testing.T) {
	var c config

	options := redisOptions(c, "127.0.0.1:6379")

	if options.DB != 0 {
		t.Fatalf("expected Redis database 0, got %d", options.DB)
	}
}

func TestAnyDeskExecutableCandidatesPrioritizeConfiguredPath(t *testing.T) {
	configured := `E:\\Apps\\AnyDesk\\AnyDesk.exe`

	candidates := anyDeskExecutableCandidates(configured)

	if candidates[0] != configured {
		t.Fatalf("expected configured executable first, got %q", candidates[0])
	}
}

func TestApplyReopenAttemptsWhenAnyDeskWasAlreadyClosed(t *testing.T) {
	called := false
	result := applyReopen(outcome{Matched: 0}, true, `E:\\Apps\\AnyDesk\\AnyDesk.exe`, func(string) bool {
		called = true
		return true
	})

	if !called {
		t.Fatal("expected AnyDesk opener to be called when reopening was requested")
	}
	if !result.ReopenAttempted {
		t.Fatal("expected reopenAttempted to be true")
	}
	if !result.Reopened {
		t.Fatal("expected reopened to report opener success")
	}
}

func TestResultPayloadCarriesCorrelationActionAndOutcome(t *testing.T) {
	completedAt := time.Date(2026, 8, 14, 10, 0, 3, 0, time.UTC)
	command := auditedCommand()

	payload := resultPayload("jcpc38", command, auditSucceeded, "", outcome{Matched: 2, ForceKilled: 2}, completedAt)

	if payload["correlationId"] != command.CorrelationID || payload["action"] != killAnyDeskAction {
		t.Fatalf("resultPayload() = %v, want the correlation id and the action", payload)
	}
	if payload["outcome"] == nil || payload["status"] != auditSucceeded {
		t.Fatalf("resultPayload() = %v, want the outcome and the status", payload)
	}
}

func TestDuplicateResultPayloadReportsNoOutcome(t *testing.T) {
	completedAt := time.Date(2026, 8, 14, 10, 0, 3, 0, time.UTC)

	payload := resultPayload("jcpc38", auditedCommand(), auditDuplicate, "commandId was already handled", nil, completedAt)

	if _, present := payload["outcome"]; present {
		t.Fatalf("resultPayload() = %v, want no outcome for a duplicate", payload)
	}
	if payload["reason"] != "commandId was already handled" {
		t.Fatalf("resultPayload() = %v, want the duplicate reason", payload)
	}
}

func TestDeadLetterPayloadKeepsTheRejectedEnvelope(t *testing.T) {
	receivedAt := time.Date(2026, 8, 14, 10, 0, 1, 0, time.UTC)

	payload := deadLetterPayload("jcpc38", `{"action":"REBOOT"}`, "action is not allowed", receivedAt)

	if payload["raw"] != `{"action":"REBOOT"}` || payload["status"] != auditRejected {
		t.Fatalf("deadLetterPayload() = %v, want the raw envelope and a rejected status", payload)
	}
}

func writeConfig(t *testing.T, allowedActions any) string {
	t.Helper()
	directory := t.TempDir()
	key := filepath.Join(directory, "id_ed25519")
	knownHosts := filepath.Join(directory, "known_hosts")
	for _, path := range []string{key, knownHosts} {
		if err := os.WriteFile(path, []byte("placeholder\n"), 0600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
	}
	raw := map[string]any{
		"machineId": "jcpc38",
		"ssh":       map[string]any{"host": "vps", "username": "remote-agent", "clientKey": key, "knownHosts": knownHosts},
		"redis":     map[string]any{},
	}
	if allowedActions != nil {
		raw["allowedActions"] = allowedActions
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}

func TestLoadConfigEnablesOnlyKillAnyDeskWhenNoActionIsListed(t *testing.T) {
	c, err := loadConfig(writeConfig(t, nil))
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}

	if len(c.enabled) != 1 {
		t.Fatalf("c.enabled = %v, want only KILL_ANYDESK", c.enabled)
	}
	if _, ok := c.enabled[killAnyDeskAction]; !ok {
		t.Fatalf("c.enabled = %v, want KILL_ANYDESK", c.enabled)
	}
}

func TestLoadConfigRejectsAnAllowedActionOutsideTheCatalog(t *testing.T) {
	_, err := loadConfig(writeConfig(t, []string{"KILL_ANYDESK", "RUN_SCRIPT"}))

	if err == nil || !strings.Contains(err.Error(), "RUN_SCRIPT") {
		t.Fatalf("loadConfig() error = %v, want the unknown action to be named", err)
	}
}

func TestLoadConfigHonorsAnEmptyAllowedActionsList(t *testing.T) {
	c, err := loadConfig(writeConfig(t, []string{}))
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}

	if len(c.enabled) != 0 {
		t.Fatalf("c.enabled = %v, want no enabled action", c.enabled)
	}
}
