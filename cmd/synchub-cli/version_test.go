package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bruceblink/SyncHub/internal/version"
)

func TestRunVersionPrintsVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		var stdout bytes.Buffer
		if err := run(context.Background(), args, &stdout, &bytes.Buffer{}); err != nil {
			t.Fatalf("run version %v: %v", args, err)
		}
		want := version.Name + " " + version.Version + "\n"
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunVersionCanOutputJSON(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"version", "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run version json: %v", err)
	}
	if strings.Contains(stdout.String(), version.Name+" ") {
		t.Fatalf("json output includes text version: %s", stdout.String())
	}
	var snapshot versionSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode version json: %v\n%s", err, stdout.String())
	}
	if snapshot.Name != version.Name || snapshot.Version != version.Version {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRunHelpIncludesVersionJSONCommand(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"help"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run help: %v", err)
	}
	if !strings.Contains(stdout.String(), "synchub-cli version --json") {
		t.Fatalf("help missing version json command: %s", stdout.String())
	}
}
