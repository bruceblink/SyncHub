package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPingCreatesStorageRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage")
	store := NewLocal(root)

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping local storage: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("stat storage root: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("storage root is not a directory")
	}
}

func TestLocalPingHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewLocal(t.TempDir())

	if err := store.Ping(ctx); err != context.Canceled {
		t.Fatalf("ping error = %v, want context.Canceled", err)
	}
}

func TestLocalRejectsTraversalStorageKeys(t *testing.T) {
	store := NewLocal(t.TempDir())
	for _, key := range []string{
		"../object",
		"objects/../secret",
		`objects\..\secret`,
		"/absolute/object",
	} {
		t.Run(key, func(t *testing.T) {
			err := store.PutChunk(context.Background(), key, strings.NewReader("x"), "")
			if err == nil || !strings.Contains(err.Error(), "invalid storage key") {
				t.Fatalf("PutChunk(%q) error = %v, want invalid storage key", key, err)
			}
		})
	}
}
