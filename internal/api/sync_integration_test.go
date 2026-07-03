package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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
	secondDeviceResp := doJSON(t, server, http.MethodPost, "/api/v1/devices", token, map[string]any{
		"name":     "desktop",
		"platform": "linux",
	})
	if secondDeviceResp.Code != http.StatusCreated {
		t.Fatalf("register second device status = %d body = %s", secondDeviceResp.Code, secondDeviceResp.Body.String())
	}

	devicesReq := httptest.NewRequest(http.MethodGet, "/api/v1/devices?limit=10", nil)
	devicesReq.Header.Set("Authorization", "Bearer "+token)
	devicesRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(devicesRec, devicesReq)
	if devicesRec.Code != http.StatusOK {
		t.Fatalf("devices status = %d body = %s", devicesRec.Code, devicesRec.Body.String())
	}
	var devicesBody struct {
		Data struct {
			Items []struct {
				ID                  string `json:"id"`
				Name                string `json:"name"`
				Platform            string `json:"platform"`
				LastAppliedChangeID int64  `json:"last_applied_change_id"`
			} `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, devicesRec, &devicesBody)
	if len(devicesBody.Data.Items) != 2 {
		t.Fatalf("devices = %#v, want two", devicesBody.Data.Items)
	}
	seenDevices := map[string]string{}
	for _, item := range devicesBody.Data.Items {
		seenDevices[item.ID] = item.Name + "/" + item.Platform
	}
	if seenDevices[deviceBody.Data.ID] != "work-laptop/windows" {
		t.Fatalf("registered device missing from list: %#v", devicesBody.Data.Items)
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
		"device_id":  deviceBody.Data.ID,
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
				ID             int64   `json:"id"`
				FileID         string  `json:"file_id"`
				EventType      string  `json:"event_type"`
				Path           string  `json:"path"`
				SourceDeviceID *string `json:"source_device_id"`
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
	if changesBody.Data.Items[1].SourceDeviceID == nil || *changesBody.Data.Items[1].SourceDeviceID != deviceBody.Data.ID {
		t.Fatalf("file change source device = %#v, want %s", changesBody.Data.Items[1].SourceDeviceID, deviceBody.Data.ID)
	}
	if changesBody.Data.NextCursor == 0 {
		t.Fatalf("missing next cursor: %#v", changesBody.Data)
	}

	moveResp := doJSON(t, server, http.MethodPatch, "/api/v1/files/"+changesBody.Data.Items[1].FileID, token, map[string]any{
		"path":      "/workspace/sync-renamed.txt",
		"device_id": deviceBody.Data.ID,
	})
	if moveResp.Code != http.StatusOK {
		t.Fatalf("move status = %d body = %s", moveResp.Code, moveResp.Body.String())
	}
	deleteResp := doJSON(t, server, http.MethodDelete, "/api/v1/files/"+changesBody.Data.Items[1].FileID, token, map[string]any{
		"device_id": deviceBody.Data.ID,
	})
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d body = %s", deleteResp.Code, deleteResp.Body.String())
	}

	sourceReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/changes?device_id="+deviceBody.Data.ID+"&after_change_id="+strconv.FormatInt(changesBody.Data.NextCursor, 10)+"&limit=10", nil)
	sourceReq.Header.Set("Authorization", "Bearer "+token)
	sourceRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(sourceRec, sourceReq)
	if sourceRec.Code != http.StatusOK {
		t.Fatalf("source changes status = %d body = %s", sourceRec.Code, sourceRec.Body.String())
	}
	var sourceBody struct {
		Data struct {
			Items []struct {
				EventType      string  `json:"event_type"`
				SourceDeviceID *string `json:"source_device_id"`
			} `json:"items"`
			NextCursor int64 `json:"next_cursor"`
		} `json:"data"`
	}
	decodeBody(t, sourceRec, &sourceBody)
	if len(sourceBody.Data.Items) != 2 {
		t.Fatalf("source changes count = %d body = %s", len(sourceBody.Data.Items), sourceRec.Body.String())
	}
	for _, item := range sourceBody.Data.Items {
		if item.EventType != "move" && item.EventType != "delete" {
			t.Fatalf("unexpected source event = %#v", item)
		}
		if item.SourceDeviceID == nil || *item.SourceDeviceID != deviceBody.Data.ID {
			t.Fatalf("%s source device = %#v, want %s", item.EventType, item.SourceDeviceID, deviceBody.Data.ID)
		}
	}
	if sourceBody.Data.NextCursor <= changesBody.Data.NextCursor {
		t.Fatalf("source next cursor = %d, want > %d", sourceBody.Data.NextCursor, changesBody.Data.NextCursor)
	}

	ackResp := doJSON(t, server, http.MethodPost, "/api/v1/sync/ack", token, map[string]any{
		"device_id":              deviceBody.Data.ID,
		"last_applied_change_id": sourceBody.Data.NextCursor,
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
	if ackBody.Data.LastAppliedChangeID != sourceBody.Data.NextCursor {
		t.Fatalf("ack cursor = %d, want %d", ackBody.Data.LastAppliedChangeID, sourceBody.Data.NextCursor)
	}

	heartbeatResp := doJSON(t, server, http.MethodPost, "/api/v1/devices/"+deviceBody.Data.ID+"/heartbeat", token, map[string]any{})
	if heartbeatResp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body = %s", heartbeatResp.Code, heartbeatResp.Body.String())
	}

	expiredCursorReq := httptest.NewRequest(http.MethodGet, "/api/v1/sync/changes?device_id="+deviceBody.Data.ID+"&after_change_id=999999&limit=10", nil)
	expiredCursorReq.Header.Set("Authorization", "Bearer "+token)
	expiredCursorRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(expiredCursorRec, expiredCursorReq)
	if expiredCursorRec.Code != http.StatusGone {
		t.Fatalf("expired cursor status = %d body = %s", expiredCursorRec.Code, expiredCursorRec.Body.String())
	}
	var expiredCursorBody struct {
		Code string `json:"code"`
	}
	decodeBody(t, expiredCursorRec, &expiredCursorBody)
	if expiredCursorBody.Code != "SYNC_CURSOR_EXPIRED" {
		t.Fatalf("expired cursor code = %q body = %s", expiredCursorBody.Code, expiredCursorRec.Body.String())
	}
}
