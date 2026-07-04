package main

import (
	"bytes"
	"context"
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
