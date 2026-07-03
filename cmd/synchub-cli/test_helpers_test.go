package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/bruceblink/SyncHub/pkg/client"
)

func clientUser(id, email string) client.User {
	return client.User{ID: id, Email: email, Status: "active"}
}

func writeTestWorkspaceConfig(t *testing.T, root string) {
	t.Helper()
	writeTestWorkspaceConfigWithServer(t, root, "http://localhost:8765")
}

func writeTestWorkspaceConfigWithServer(t *testing.T, root, serverURL string) {
	t.Helper()
	writeTestWorkspaceConfigValue(t, root, workspaceConfig{
		Version:    1,
		Root:       root,
		RemotePath: "/workspace",
		ServerURL:  serverURL,
		UserID:     "u1",
		UserEmail:  "user@example.com",
	})
}

func writeTestWorkspaceConfigValue(t *testing.T, root string, cfg workspaceConfig) {
	t.Helper()
	if err := writeJSONFile(filepath.Join(root, ".synchub", "workspace.json"), cfg, 0o600); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
}

func testSHA(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
