package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRunCapabilities_EmitsContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCapabilities(&stdout, &stderr, cliFlags{})
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty, got: %s", stderr.String())
	}

	var env struct {
		Status string         `json:"status"`
		Count  int            `json:"count"`
		Data   []capabilities `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, stdout.String())
	}
	if env.Status != "ok" {
		t.Errorf("status = %q", env.Status)
	}
	if len(env.Data) != 1 {
		t.Fatalf("expected 1 capabilities entry, got %d", len(env.Data))
	}
	caps := env.Data[0]
	if caps.SchemaVersion != CapabilitiesSchemaVersion {
		t.Errorf("schema_version = %q, want %q", caps.SchemaVersion, CapabilitiesSchemaVersion)
	}
	if caps.Tool != "golazo" {
		t.Errorf("tool = %q, want golazo", caps.Tool)
	}
}

func TestCapabilities_EnumeratesAllSubcommands(t *testing.T) {
	caps := buildCapabilities()
	want := map[string]bool{
		"live":         false,
		"finished":     false,
		"match":        false,
		"leagues":      false,
		"capabilities": false,
	}
	for _, cmd := range caps.Commands {
		if _, ok := want[cmd.Name]; ok {
			want[cmd.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q missing from capabilities payload", name)
		}
	}
}

func TestCapabilities_ErrorCodesMatchExitCodes(t *testing.T) {
	caps := buildCapabilities()
	// The error_codes map must be consistent with cli_output.go's ExitCodeFor.
	for code, exit := range caps.ErrorCodes {
		got := ExitCodeFor(ErrorCode(code))
		if got != exit {
			t.Errorf("error_codes[%q] = %d, but ExitCodeFor returns %d", code, exit, got)
		}
	}
}

func TestCapabilities_EveryCommandHasExample(t *testing.T) {
	caps := buildCapabilities()
	for _, cmd := range caps.Commands {
		if cmd.Example == "" {
			t.Errorf("command %q has empty example — agents rely on this", cmd.Name)
		}
		if cmd.Description == "" {
			t.Errorf("command %q has empty description", cmd.Name)
		}
	}
}
