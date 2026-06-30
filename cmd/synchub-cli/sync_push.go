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

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

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
	for _, item := range m.Items {
		if err := pushManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, root, item, createdDirs); err != nil {
			return err
		}
		uploaded++
	}
	fmt.Fprintf(stdout, "uploaded: %d files\n", uploaded)
	return nil
}

func pushManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, root string, item manifest.Entry, createdDirs map[string]struct{}) error {
	localPath := filepath.Join(root, filepath.FromSlash(item.RelativePath))
	if err := ensureRemoteDirectories(ctx, apiClient, accessToken, item.Path, createdDirs); err != nil {
		return err
	}
	session, err := apiClient.InitUpload(ctx, accessToken, client.InitUploadRequest{
		Path:        item.Path,
		Size:        item.Size,
		SHA256:      item.SHA256,
		BaseVersion: item.RemoteVersion,
	}, uploadIdempotencyKey(item))
	if err != nil {
		return err
	}
	if err := uploadFileChunks(ctx, apiClient, accessToken, session.UploadID, localPath, session.ChunkSize); err != nil {
		return err
	}
	_, err = apiClient.CommitUpload(ctx, accessToken, session.UploadID)
	return err
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
