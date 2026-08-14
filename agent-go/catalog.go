package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	killAnyDeskAction  = "KILL_ANYDESK"
	maxRequesterLength = 64
)

// actionSpec is the compiled description of one remote command. Entries exist only in
// this file: nothing that arrives on the queue can register, rename or reconfigure an
// action, and no handler ever receives received text as an executable, a command line
// or a shell string.
type actionSpec struct {
	name             string
	idempotent       bool
	maxLifetime      time.Duration
	enabledByDefault bool
	parseArgs        func(json.RawMessage) (any, error)
	execute          func(config, any) (any, error)
}

// actionSet is the subset of the catalog a given machine accepts.
type actionSet map[string]actionSpec

type killAnyDeskArgs struct {
	ReopenAnyDesk bool
}

var catalog = map[string]actionSpec{
	killAnyDeskAction: {
		name:             killAnyDeskAction,
		idempotent:       true,
		maxLifetime:      15 * time.Minute,
		enabledByDefault: true,
		parseArgs:        parseKillAnyDeskArgs,
		execute:          executeKillAnyDesk,
	},
}

// defaultActions is what a machine accepts when config.json does not list actions.
// A new catalog entry stays inert until an operator enables it on a specific machine.
func defaultActions() actionSet {
	actions := actionSet{}
	for name, spec := range catalog {
		if spec.enabledByDefault {
			actions[name] = spec
		}
	}
	return actions
}

// configuredActions intersects the machine allowlist with the compiled catalog.
func configuredActions(names []string) (actionSet, error) {
	actions := actionSet{}
	for _, name := range names {
		spec, known := catalog[name]
		if !known {
			return nil, fmt.Errorf("allowedActions contains %q, which is not in the compiled catalog", name)
		}
		actions[name] = spec
	}
	return actions, nil
}

// argumentFields decodes the arguments object and rejects every key the action did not declare.
func argumentFields(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("arguments must be an object")
	}
	for name := range fields {
		if !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("argument %q is not allowed for this action", name)
		}
	}
	return fields, nil
}

func parseKillAnyDeskArgs(raw json.RawMessage) (any, error) {
	fields, err := argumentFields(raw, "reopenAnyDesk")
	if err != nil {
		return nil, err
	}
	var args killAnyDeskArgs
	if value, ok := fields["reopenAnyDesk"]; ok {
		if err := json.Unmarshal(value, &args.ReopenAnyDesk); err != nil {
			return nil, errors.New("reopenAnyDesk must be a boolean")
		}
	}
	return args, nil
}

func executeKillAnyDesk(c config, arguments any) (any, error) {
	args, ok := arguments.(killAnyDeskArgs)
	if !ok {
		return nil, errors.New("arguments do not match KILL_ANYDESK")
	}
	result, err := executeKill()
	if err != nil {
		return nil, err
	}
	return applyReopen(result, args.ReopenAnyDesk, c.AnyDeskExecutablePath, reopenAnyDesk), nil
}
