package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	authsvc "github.com/bruceblink/SyncHub/internal/auth"
	"github.com/bruceblink/SyncHub/internal/db"
	filesvc "github.com/bruceblink/SyncHub/internal/file"
	"github.com/bruceblink/SyncHub/internal/storage"
	syncsvc "github.com/bruceblink/SyncHub/internal/sync"
)

func TestSQLiteSyncDeviceAndChangeFeed(t *testing.T) {
	ctx := context.Background()
	repo, err := db.OpenSQLite(ctx, filepath.Join(t.TempDir(), "synchub.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	authService := authsvc.NewService(repo, "test-secret", 15*time.Minute, 24*time.Hour)
	store := storage.NewLocal(t.TempDir())
	fileService := filesvc.NewService(repo, store, 4*1024*1024, 24*time.Hour)
	syncService := syncsvc.NewService(repo)
	server := NewWithSync(authService, fileService, syncService, repo)

	registerResp := doJSON(t, server, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":    "sync@example.com",
		"password": "password123",
	})
	if registerResp.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", registerResp.Code, registerResp.Body.String())
	}
	var registerBody struct {
		Data struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		} `json:"data"`
	}
	decodeBody(t, registerResp, &registerBody)
	token := registerBody.Data.Tokens.AccessToken

	deviceResp := doJSON(t, server, http.MethodPost, "/api/v1/devices", token, map[string]any{
		"name":     "work-laptop",
		"platform": "windows",
	})
	if deviceResp.Code != http.StatusCreated {
		t.Fatalf("register device status = %d body = %s", deviceResp.Code, deviceResp.Body.String())
	}
	var deviceBody struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decodeBody(t, deviceResp, &deviceBody)
	if deviceBody.Data.ID == "" {
		t.Fatal("device response missing id")
	}

	createDirResp := doJSON(t, server, http.MethodPost, "/api/v1/files/directories", token, map[string]any{"path": "/workspace"})
	if createDirResp.Code != http.StatusCreated {
		t.Fatalf("create directory status = %d body = %s", createDirResp.Code, createDirResp.Body.String())
	}

	content := []byte("hello sync")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	uploadResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads", token, map[string]any{
		"path":       "/workspace/sync.txt",
		"size":       len(content),
		"sha256":     sha,
		"chunk_size": len(content),
	})
	if uploadResp.Code != http.StatusCreated {
		t.Fatalf("init upload status = %d body = %s", uploadResp.Code, uploadResp.Body.String())
	}
	var uploadBody struct {
		Data struct {
			UploadID string `json:"upload_id"`
		} `json:"data"`
	}
	decodeBody(t, uploadResp, &uploadBody)

	chunkReq := httptest.NewRequest(http.MethodPut, "/api/v1/uploads/"+uploadBody.Data.UploadID+"/chunks/0", bytes.NewReader(content))
	chunkReq.Header.Set("Authorization", "Bearer "+token)
	chunkReq.Header.Set("Content-Type", "application/octet-stream")
	chunkReq.Header.Set("X-Chunk-Sha256", sha)
	chunkRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(chunkRec, chunkReq)
	if chunkRec.Code != http.StatusOK {
		t.Fatalf("put chunk status = %d body = %s", chunkRec.Code, chunkRec.Body.String())
	}

	commitResp := doJSON(t, server, http.MethodPost, "/api/v1/uploads/"+uploadBody.Data.UploadID+"/commit", token, map[string]any{})
	if commitResp.Code != http.StatusOK {
		t.Fatalf("commit upload status = %d body = %s", commitResp.Code, commitResp.Body.String())
	}

	changesReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/changes?device_id="+deviceBody.Data.ID+"&after_change_id=0&limit=10", nil)
	changesReq.Header.Set("Authorization", "Bearer "+token)
	changesRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(changesRec, changesReq)
	if changesRec.Code != http.StatusOK {
		t.Fatalf("changes status = %d body = %s", changesRec.Code, changesRec.Body.String())
	}
	var changesBody struct {
		Data struct {
			Items []struct {
				ID        int64  `json:"id"`
				EventType string `json:"event_type"`
				Path      string `json:"path"`
			} `json:"items"`
			NextCursor int64 `json:"next_cursor"`
		} `json:"data"`
	}
	decodeBody(t, changesRec, &changesBody)
	if len(changesBody.Data.Items) != 2 {
		t.Fatalf("changes count = %d body = %s", len(changesBody.Data.Items), changesRec.Body.String())
	}
	if changesBody.Data.Items[0].EventType != "create" || changesBody.Data.Items[0].Path != "/workspace" {
		t.Fatalf("directory change = %#v", changesBody.Data.Items[0])
	}
	if changesBody.Data.Items[1].EventType != "create" || changesBody.Data.Items[1].Path != "/workspace/sync.txt" {
		t.Fatalf("file change = %#v", changesBody.Data.Items[1])
	}
	if changesBody.Data.NextCursor == 0 {
		t.Fatalf("missing next cursor: %#v", changesBody.Data)
	}

	ackResp := doJSON(t, server, http.MethodPost, "/api/v1/sync/ack", token, map[string]any{
		"device_id":              deviceBody.Data.ID,
		"last_applied_change_id": changesBody.Data.NextCursor,
	})
	if ackResp.Code != http.StatusOK {
		t.Fatalf("ack status = %d body = %s", ackResp.Code, ackResp.Body.String())
	}
	var ackBody struct {
		Data struct {
			LastAppliedChangeID int64 `json:"last_applied_change_id"`
		} `json:"data"`
	}
	decodeBody(t, ackResp, &ackBody)
	if ackBody.Data.LastAppliedChangeID != changesBody.Data.NextCursor {
		t.Fatalf("ack cursor = %d, want %d", ackBody.Data.LastAppliedChangeID, changesBody.Data.NextCursor)
	}

	heartbeatResp := doJSON(t, server, http.MethodPost, "/api/v1/devices/"+deviceBody.Data.ID+"/heartbeat", token, map[string]any{})
	if heartbeatResp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body = %s", heartbeatResp.Code, heartbeatResp.Body.String())
	}
}
