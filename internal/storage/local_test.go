package storage

import (
	"context"
	"os"
	"path/filepath"
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
