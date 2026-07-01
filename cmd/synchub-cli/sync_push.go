package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

var syncPushNow = time.Now

type pushManifestResult struct {
	conflictKept bool
	version      int64
}

func runSyncPush(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, workspace, localManifestPath, err := loadWorkspaceAndManifestPath(*rootPath, *workspaceConfigPath, *manifestPath)
	if err != nil {
		return err
	}
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("manifest is missing; run synchub-cli manifest scan first")
		}
		return err
	}
	if m.Root != "" && filepath.Clean(m.Root) != filepath.Clean(root) {
		return fmt.Errorf("manifest root %s does not match workspace root %s", m.Root, root)
	}
	loginConfig, err := readConfig(*configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	apiClient := client.New(serverURL)
	createdDirs := map[string]struct{}{}
	uploaded := 0
	deleted := 0
	conflictKept := 0
	manifestChanged := false
	keptItems := make([]manifest.Entry, 0, len(m.Items))
	for _, item := range m.Items {
		exists, err := manifestEntryLocalFileExists(root, item)
		if err != nil {
			return err
		}
		if !exists {
			if err := deleteManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, item); err != nil {
				return err
			}
			deleted++
			manifestChanged = true
			continue
		}
		result, err := pushManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, root, workspace, item, createdDirs)
		if err != nil {
			return err
		}
		uploaded++
		if result.version > 0 && (item.RemoteVersion == nil || *item.RemoteVersion != result.version) {
			version := result.version
			item.RemoteVersion = &version
			manifestChanged = true
		}
		if result.conflictKept {
			conflictKept++
		}
		keptItems = append(keptItems, item)
	}
	if manifestChanged {
		m.Items = keptItems
		if err := writeManifest(localManifestPath, m); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "uploaded: %d files\n", uploaded)
	if deleted > 0 {
		fmt.Fprintf(stdout, "deleted: %d files\n", deleted)
	}
	if conflictKept > 0 {
		fmt.Fprintf(stdout, "conflicts kept: %d\n", conflictKept)
	}
	return nil
}

func pushManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, root string, workspace workspaceConfig, item manifest.Entry, createdDirs map[string]struct{}) (pushManifestResult, error) {
	version, err := uploadManifestEntry(ctx, apiClient, accessToken, root, item, createdDirs)
	if err != nil {
		if !isAPIErrorCode(err, "FILE_CONFLICT") {
			return pushManifestResult{}, err
		}
		conflictItem := item
		conflictItem.Path = conflictRemotePath(item.Path, conflictDeviceLabel(workspace), syncPushNow().UTC())
		conflictItem.RemoteVersion = nil
		if _, err := uploadManifestEntry(ctx, apiClient, accessToken, root, conflictItem, createdDirs); err != nil {
			return pushManifestResult{}, fmt.Errorf("upload conflict copy for %s: %w", item.Path, err)
		}
		return pushManifestResult{conflictKept: true}, nil
	}
	return pushManifestResult{version: version}, nil
}

func manifestEntryLocalFileExists(root string, item manifest.Entry) (bool, error) {
	localPath := filepath.Join(root, filepath.FromSlash(item.RelativePath))
	if err := ensureLocalPathInsideRoot(root, localPath); err != nil {
		return false, err
	}
	info, err := os.Stat(localPath)
	if err == nil {
		return info.Mode().IsRegular(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func deleteManifestEntry(ctx context.Context, apiClient *client.Client, accessToken string, item manifest.Entry) error {
	node, err := apiClient.GetFileByPath(ctx, accessToken, item.Path)
	if err != nil {
		if isAPIErrorCode(err, "NOT_FOUND") {
			return nil
		}
		return err
	}
	if err := apiClient.DeleteFile(ctx, accessToken, node.ID); err != nil && !isAPIErrorCode(err, "NOT_FOUND") {
		return err
	}
	return nil
}

func uploadManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, root string, item manifest.Entry, createdDirs map[string]struct{}) (int64, error) {
	localPath := filepath.Join(root, filepath.FromSlash(item.RelativePath))
	if err := ensureRemoteDirectories(ctx, apiClient, accessToken, item.Path, createdDirs); err != nil {
		return 0, err
	}
	session, err := apiClient.InitUpload(ctx, accessToken, client.InitUploadRequest{
		Path:        item.Path,
		Size:        item.Size,
		SHA256:      item.SHA256,
		BaseVersion: item.RemoteVersion,
	}, uploadIdempotencyKey(item))
	if err != nil {
		return 0, err
	}
	if err := uploadFileChunks(ctx, apiClient, accessToken, session.UploadID, localPath, session.ChunkSize); err != nil {
		return 0, err
	}
	commit, err := apiClient.CommitUpload(ctx, accessToken, session.UploadID)
	if err != nil {
		return 0, err
	}
	return commit.Version, nil
}

func conflictRemotePath(remotePath, device string, timestamp time.Time) string {
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(remotePath, "\\", "/"), "/"))
	dir := pathpkg.Dir(cleaned)
	base := pathpkg.Base(cleaned)
	ext := pathpkg.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = strings.TrimPrefix(base, ".")
		ext = ""
	}
	conflictName := fmt.Sprintf("%s.conflict-%s-%s%s", name, sanitizeConflictPathPart(device), timestamp.UTC().Format("20060102T150405.000000000Z"), ext)
	if dir == "." || dir == "/" {
		return "/" + conflictName
	}
	return pathpkg.Join(dir, conflictName)
}

func conflictDeviceLabel(workspace workspaceConfig) string {
	if label := strings.TrimSpace(workspace.DeviceID); label != "" {
		return label
	}
	if label := strings.TrimSpace(workspace.DeviceName); label != "" {
		return label
	}
	return "device"
}

func sanitizeConflictPathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "device"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-',
			r == '_',
			r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	sanitized := strings.Trim(b.String(), "._-")
	if sanitized == "" {
		return "device"
	}
	return sanitized
}

func ensureRemoteDirectories(ctx context.Context, apiClient *client.Client, accessToken, filePath string, created map[string]struct{}) error {
	for _, dir := range remoteParentDirs(filePath) {
		if _, ok := created[dir]; ok {
			continue
		}
		if _, err := apiClient.CreateDirectory(ctx, accessToken, dir); err != nil && !isAPIErrorCode(err, "ALREADY_EXISTS") {
			return err
		}
		created[dir] = struct{}{}
	}
	return nil
}

func remoteParentDirs(filePath string) []string {
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(filePath, "\\", "/"), "/"))
	dir := pathpkg.Dir(cleaned)
	if dir == "." || dir == "/" {
		return nil
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	dirs := make([]string, 0, len(parts))
	current := ""
	for _, part := range parts {
		current = pathpkg.Join(current, part)
		dirs = append(dirs, "/"+strings.TrimPrefix(current, "/"))
	}
	return dirs
}

func uploadFileChunks(ctx context.Context, apiClient *client.Client, accessToken, uploadID, localPath string, chunkSize int64) error {
	if chunkSize <= 0 {
		return errors.New("server returned invalid chunk size")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return uploadChunk(ctx, apiClient, accessToken, uploadID, 0, nil)
	}

	buf := make([]byte, int(chunkSize))
	var index int32
	for {
		n, readErr := io.ReadFull(f, buf)
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		if err := uploadChunk(ctx, apiClient, accessToken, uploadID, index, buf[:n]); err != nil {
			return err
		}
		index++
		if readErr == io.ErrUnexpectedEOF {
			return nil
		}
	}
}

func uploadChunk(ctx context.Context, apiClient *client.Client, accessToken, uploadID string, index int32, data []byte) error {
	sum := sha256.Sum256(data)
	_, err := apiClient.PutUploadChunk(ctx, accessToken, uploadID, index, bytes.NewReader(data), hex.EncodeToString(sum[:]))
	return err
}

func uploadIdempotencyKey(item manifest.Entry) string {
	return "cli-push:" + item.Path + ":" + item.SHA256
}

func isAPIErrorCode(err error, code string) bool {
	var apiErr *client.Error
	if errors.As(err, &apiErr) {
		if got, ok := apiErr.Code.(string); ok {
			return got == code
		}
	}
	return false
}
