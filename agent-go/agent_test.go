package main

import (
	"strings"
	"testing"
	"time"
)

func TestValidateCommandAcceptsCurrentTargetAndFutureExpiry(t *testing.T) {
	now := time.Now().UTC()
	raw := `{"version":1,"commandId":"8ddc0c10-253a-4e37-895f-c4b749e1c312","correlationId":"d840886f-3684-4180-b8b2-97c0402e6c69","target":"jcpc38","action":"KILL_ANYDESK","args":{},"requestedAt":"` + now.Add(-time.Minute).Format(time.RFC3339) + `","expiresAt":"` + now.Add(time.Minute).Format(time.RFC3339) + `"}`

	command, _, err := validateCommand(raw, "jcpc38", defaultActions(), now)
	if err != nil {
		t.Fatalf("validateCommand() error = %v", err)
	}
	if command.Action != killAnyDeskAction || command.Target != "jcpc38" {
		t.Fatalf("validateCommand() = %+v", command)
	}
}

func TestValidateCommandRejectsWrongTarget(t *testing.T) {
	now := time.Now().UTC()
	raw := `{"version":1,"commandId":"8ddc0c10-253a-4e37-895f-c4b749e1c312","correlationId":"d840886f-3684-4180-b8b2-97c0402e6c69","target":"other-pc","action":"KILL_ANYDESK","args":{},"requestedAt":"` + now.Add(-time.Minute).Format(time.RFC3339) + `","expiresAt":"` + now.Add(time.Minute).Format(time.RFC3339) + `"}`

	_, _, err := validateCommand(raw, "jcpc38", defaultActions(), now)
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("validateCommand() error = %v, want target error", err)
	}
}

func TestKillAnyDeskSkipsTaskkillWhenNoProcessMatches(t *testing.T) {
	called := false
	runner := func(name string, args ...string) (string, error) {
		called = true
		return "", nil
	}

	outcome, err := killAnyDesk(runner, "Image Name,PID\r\n")
	if err != nil {
		t.Fatalf("killAnyDesk() error = %v", err)
	}
	if called || outcome.Matched != 0 || outcome.ForceKilled != 0 {
		t.Fatalf("killAnyDesk() = %+v, runner called = %t", outcome, called)
	}
}
