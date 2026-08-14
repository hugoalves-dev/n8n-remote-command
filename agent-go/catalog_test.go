package main

import (
	"strings"
	"testing"
	"time"
)

func envelope(t *testing.T, action, args string, lifetime time.Duration, now time.Time) string {
	t.Helper()
	requestedAt := now.Add(-time.Second)
	return `{"version":1,"commandId":"8ddc0c10-253a-4e37-895f-c4b749e1c312",` +
		`"correlationId":"d840886f-3684-4180-b8b2-97c0402e6c69","target":"jcpc38","action":"` + action +
		`","args":` + args + `,"requestedBy":"n8n","requestedAt":"` + requestedAt.Format(time.RFC3339) +
		`","expiresAt":"` + requestedAt.Add(lifetime).Format(time.RFC3339) + `"}`
}

func TestCatalogRejectsUnknownAction(t *testing.T) {
	now := time.Now().UTC()

	_, _, err := validateCommand(envelope(t, "REBOOT", `{}`, time.Minute, now), "jcpc38", defaultActions(), now)

	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("validateCommand() error = %v, want an action-not-allowed error", err)
	}
}

func TestCatalogRejectsActionThatIsNotEnabledOnThisMachine(t *testing.T) {
	now := time.Now().UTC()

	_, _, err := validateCommand(envelope(t, killAnyDeskAction, `{}`, time.Minute, now), "jcpc38", actionSet{}, now)

	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("validateCommand() error = %v, want a not-authorized error", err)
	}
}

func TestValidateCommandReturnsTheCatalogEntryForTheAction(t *testing.T) {
	now := time.Now().UTC()

	command, spec, err := validateCommand(envelope(t, killAnyDeskAction, `{"reopenAnyDesk":true}`, time.Minute, now), "jcpc38", defaultActions(), now)
	if err != nil {
		t.Fatalf("validateCommand() error = %v", err)
	}

	if spec.name != killAnyDeskAction {
		t.Fatalf("spec.name = %q, want %q", spec.name, killAnyDeskAction)
	}
	arguments, ok := command.arguments.(killAnyDeskArgs)
	if !ok || !arguments.ReopenAnyDesk {
		t.Fatalf("command.arguments = %#v, want killAnyDeskArgs{ReopenAnyDesk: true}", command.arguments)
	}
}

func TestKillAnyDeskRejectsUnknownArgument(t *testing.T) {
	now := time.Now().UTC()

	_, _, err := validateCommand(envelope(t, killAnyDeskAction, `{"executable":"C:\\evil.exe"}`, time.Minute, now), "jcpc38", defaultActions(), now)

	if err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("validateCommand() error = %v, want the unknown argument to be named", err)
	}
}

func TestKillAnyDeskRejectsNonBooleanReopenArgument(t *testing.T) {
	now := time.Now().UTC()

	_, _, err := validateCommand(envelope(t, killAnyDeskAction, `{"reopenAnyDesk":"yes"}`, time.Minute, now), "jcpc38", defaultActions(), now)

	if err == nil || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("validateCommand() error = %v, want a boolean type error", err)
	}
}

func TestValidateCommandRejectsLifetimeBeyondTheActionMaximum(t *testing.T) {
	now := time.Now().UTC()
	lifetime := catalog[killAnyDeskAction].maxLifetime + time.Minute

	_, _, err := validateCommand(envelope(t, killAnyDeskAction, `{}`, lifetime, now), "jcpc38", defaultActions(), now)

	if err == nil || !strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("validateCommand() error = %v, want a lifetime ceiling error", err)
	}
}

func TestKillAnyDeskIsDeclaredIdempotent(t *testing.T) {
	if !catalog[killAnyDeskAction].idempotent {
		t.Fatal("KILL_ANYDESK must declare its idempotency semantics as idempotent")
	}
}

func TestDefaultActionsOnlyContainKillAnyDesk(t *testing.T) {
	actions := defaultActions()

	if len(actions) != 1 {
		t.Fatalf("defaultActions() = %v, want only KILL_ANYDESK", actions)
	}
	if _, ok := actions[killAnyDeskAction]; !ok {
		t.Fatalf("defaultActions() = %v, want KILL_ANYDESK", actions)
	}
}

func TestConfiguredActionsRejectAnActionThatIsNotInTheCatalog(t *testing.T) {
	_, err := configuredActions([]string{"RUN_SCRIPT"})

	if err == nil || !strings.Contains(err.Error(), "RUN_SCRIPT") {
		t.Fatalf("configuredActions() error = %v, want the unknown action to be named", err)
	}
}

func TestConfiguredActionsKeepOnlyTheListedActions(t *testing.T) {
	actions, err := configuredActions([]string{killAnyDeskAction})
	if err != nil {
		t.Fatalf("configuredActions() error = %v", err)
	}

	if len(actions) != 1 {
		t.Fatalf("configuredActions() = %v, want only KILL_ANYDESK", actions)
	}
}

func TestConfiguredActionsAcceptAnEmptyList(t *testing.T) {
	actions, err := configuredActions([]string{})
	if err != nil {
		t.Fatalf("configuredActions() error = %v", err)
	}

	if len(actions) != 0 {
		t.Fatalf("configuredActions() = %v, want no enabled action", actions)
	}
}

func TestValidateCommandRejectsAnOversizedRequester(t *testing.T) {
	now := time.Now().UTC()
	raw := strings.Replace(envelope(t, killAnyDeskAction, `{}`, time.Minute, now), `"requestedBy":"n8n"`,
		`"requestedBy":"`+strings.Repeat("a", maxRequesterLength+1)+`"`, 1)

	_, _, err := validateCommand(raw, "jcpc38", defaultActions(), now)

	if err == nil || !strings.Contains(err.Error(), "requestedBy") {
		t.Fatalf("validateCommand() error = %v, want a requestedBy length error", err)
	}
}
