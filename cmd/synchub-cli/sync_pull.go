package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/internal/manifest"
	"github.com/bruceblink/SyncHub/pkg/client"
)

func runSyncPull(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runSyncPullWithDeviceEnsure(ctx, args, stdout, stderr, true)
}

func runSyncPullWithDeviceEnsure(ctx context.Context, args []string, stdout, stderr io.Writer, ensureDevice bool) error {
	fs := flag.NewFlagSet("sync pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	deviceName := fs.String("device-name", "", "device name")
	devicePlatform := fs.String("platform", runtime.GOOS, "device platform")
	limit := fs.Int("limit", 500, "maximum changes to pull")
	resetCursor := fs.Bool("reset-cursor", false, "reset local change cursor and replay the available change feed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be positive")
	}

	root, workspace, workspacePath, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	localManifestPath := *manifestPath
	if strings.TrimSpace(localManifestPath) == "" {
		localManifestPath = filepath.Join(root, ".synchub", "manifest.json")
	}
	previousManifest, err := readPullManifest(root, localManifestPath)
	if err != nil {
		return err
	}
	loginConfig, err := readConfigWithRefresh(ctx, *configPath)
	if err != nil {
		return err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	apiClient := client.New(serverURL)
	if ensureDevice {
		changed, err := ensureWorkspaceDevice(ctx, apiClient, loginConfig.Tokens.AccessToken, root, &workspace, *deviceName, *devicePlatform)
		if err != nil {
			return err
		}
		if changed {
			if err := writeWorkspaceConfig(workspacePath, workspace); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(workspace.DeviceID) == "" {
		return errors.New("workspace device is not registered")
	}

	afterChangeID := workspace.LastAppliedChangeID
	if *resetCursor {
		afterChangeID = 0
	}
	changes, err := apiClient.ListChanges(ctx, loginConfig.Tokens.AccessToken, workspace.DeviceID, afterChangeID, int32(*limit))
	if err != nil {
		if isAPIErrorCode(err, "SYNC_CURSOR_EXPIRED") {
			return fmt.Errorf("sync cursor expired; run synchub-cli sync pull --path %s --reset-cursor to replay the available change feed", root)
		}
		return err
	}
	previousEntries, err := manifestEntriesByPath(previousManifest)
	if err != nil {
		return err
	}
	files, dirs, deleted, moved, conflictKept := 0, 0, 0, 0, 0
	for _, event := range changes.Items {
		if isOwnChangeEvent(workspace, event) {
			continue
		}
		result, err := applyChangeEvent(ctx, apiClient, loginConfig.Tokens.AccessToken, root, workspace, event, previousEntries)
		if err != nil {
			return err
		}
		files += result.files
		dirs += result.dirs
		deleted += result.deleted
		moved += result.moved
		conflictKept += result.conflictKept
	}
	if len(changes.Items) > 0 {
		if err := writePullManifest(ctx, root, workspace.RemotePath, localManifestPath, previousManifest, changes.Items); err != nil {
			return err
		}
	}
	nextCursor := changes.NextCursor
	if nextCursor == 0 && len(changes.Items) > 0 {
		nextCursor = changes.Items[len(changes.Items)-1].ID
	}
	if nextCursor > workspace.LastAppliedChangeID || (*resetCursor && nextCursor > 0) {
		device, err := apiClient.AckChanges(ctx, loginConfig.Tokens.AccessToken, workspace.DeviceID, nextCursor)
		if err != nil {
			return err
		}
		workspace.LastAppliedChangeID = device.LastAppliedChangeID
		if workspace.LastAppliedChangeID < nextCursor {
			workspace.LastAppliedChangeID = nextCursor
		}
		if err := writeWorkspaceConfig(workspacePath, workspace); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "pulled: %d files\n", files)
	fmt.Fprintf(stdout, "directories: %d\n", dirs)
	fmt.Fprintf(stdout, "deleted: %d\n", deleted)
	fmt.Fprintf(stdout, "moved: %d\n", moved)
	if conflictKept > 0 {
		fmt.Fprintf(stdout, "conflicts kept: %d\n", conflictKept)
	}
	fmt.Fprintf(stdout, "cursor: %d\n", workspace.LastAppliedChangeID)
	return nil
}

func isOwnChangeEvent(workspace workspaceConfig, event client.ChangeEvent) bool {
	return strings.TrimSpace(workspace.DeviceID) != "" && event.SourceDeviceID != nil && *event.SourceDeviceID == workspace.DeviceID
}

func readPullManifest(root, manifestPath string) (manifest.Manifest, error) {
	m, err := readManifest(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return manifest.Manifest{}, nil
		}
		return manifest.Manifest{}, err
	}
	if m.Root != "" && filepath.Clean(m.Root) != filepath.Clean(root) {
		return manifest.Manifest{}, fmt.Errorf("manifest root %s does not match workspace root %s", m.Root, root)
	}
	return m, nil
}

func writePullManifest(ctx context.Context, root, remoteRoot, manifestPath string, previous manifest.Manifest, changes []client.ChangeEvent) error {
	current, err := manifest.Scan(ctx, root, remoteRoot)
	if err != nil {
		return err
	}
	if err := mergePullManifestVersions(&current, previous, changes); err != nil {
		return err
	}
	return writeManifest(manifestPath, current)
}

func mergePullManifestVersions(current *manifest.Manifest, previous manifest.Manifest, changes []client.ChangeEvent) error {
	versions := make(map[string]int64, len(previous.Items))
	for _, item := range previous.Items {
		if item.RemoteVersion == nil {
			continue
		}
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return err
		}
		versions[path] = *item.RemoteVersion
	}
	for _, event := range changes {
		path, err := normalizeRemotePath(event.Path)
		if err != nil {
			return err
		}
		switch event.EventType {
		case "create", "update", "restore":
			if event.Version != nil {
				versions[path] = *event.Version
			}
		case "delete":
			removeManifestVersionsAt(versions, path)
		case "move":
			if event.OldPath != nil {
				oldPath, err := normalizeRemotePath(*event.OldPath)
				if err != nil {
					return err
				}
				moveManifestVersions(versions, oldPath, path)
			}
			if event.Version != nil {
				versions[path] = *event.Version
			}
		}
	}
	for i := range current.Items {
		path, err := normalizeRemotePath(current.Items[i].Path)
		if err != nil {
			return err
		}
		if version, ok := versions[path]; ok {
			version := version
			current.Items[i].RemoteVersion = &version
		}
	}
	return nil
}

func removeManifestVersionsAt(versions map[string]int64, remotePath string) {
	for path := range versions {
		if path == remotePath || strings.HasPrefix(path, remotePath+"/") {
			delete(versions, path)
		}
	}
}

func moveManifestVersions(versions map[string]int64, oldPath, newPath string) {
	moved := map[string]int64{}
	for path, version := range versions {
		switch {
		case path == oldPath:
			moved[newPath] = version
			delete(versions, path)
		case strings.HasPrefix(path, oldPath+"/"):
			moved[newPath+strings.TrimPrefix(path, oldPath)] = version
			delete(versions, path)
		}
	}
	for path, version := range moved {
		versions[path] = version
	}
}

func ensureWorkspaceDevice(ctx context.Context, apiClient *client.Client, accessToken, root string, workspace *workspaceConfig, deviceName, platform string) (bool, error) {
	if strings.TrimSpace(deviceName) == "" {
		deviceName = defaultDeviceName(root)
	}
	if strings.TrimSpace(platform) == "" {
		platform = runtime.GOOS
	}
	if strings.TrimSpace(workspace.DeviceID) == "" {
		device, err := apiClient.RegisterDevice(ctx, accessToken, deviceName, platform)
		if err != nil {
			return false, err
		}
		workspace.DeviceID = device.ID
		workspace.DeviceName = device.Name
		workspace.DevicePlatform = device.Platform
		workspace.LastAppliedChangeID = device.LastAppliedChangeID
		return true, nil
	}
	device, err := apiClient.HeartbeatDevice(ctx, accessToken, workspace.DeviceID)
	if err != nil {
		return false, err
	}
	changed := false
	if device.LastAppliedChangeID > workspace.LastAppliedChangeID {
		workspace.LastAppliedChangeID = device.LastAppliedChangeID
		changed = true
	}
	if workspace.DeviceName == "" && device.Name != "" {
		workspace.DeviceName = device.Name
		changed = true
	}
	if workspace.DevicePlatform == "" && device.Platform != "" {
		workspace.DevicePlatform = device.Platform
		changed = true
	}
	return changed, nil
}

type pullApplyResult struct {
	files        int
	dirs         int
	deleted      int
	moved        int
	conflictKept int
}

func applyChangeEvent(ctx context.Context, apiClient *client.Client, accessToken, root string, workspace workspaceConfig, event client.ChangeEvent, previousEntries map[string]manifest.Entry) (pullApplyResult, error) {
	switch event.EventType {
	case "create", "update", "restore":
		localPath, ok, err := localPathForRemote(root, workspace.RemotePath, event.Path)
		if err != nil || !ok {
			return pullApplyResult{}, err
		}
		if event.Version == nil {
			if err := os.MkdirAll(localPath, 0o755); err != nil {
				return pullApplyResult{}, err
			}
			return pullApplyResult{dirs: 1}, nil
		}
		conflictKept, err := keepLocalConflictIfChanged(localPath, event.Path, workspace, previousEntries)
		if err != nil {
			return pullApplyResult{}, err
		}
		if err := downloadChangeFile(ctx, apiClient, accessToken, event.FileID, localPath); err != nil {
			return pullApplyResult{}, err
		}
		return pullApplyResult{files: 1, conflictKept: boolToInt(conflictKept)}, nil
	case "delete":
		localPath, ok, err := localPathForRemote(root, workspace.RemotePath, event.Path)
		if err != nil || !ok {
			return pullApplyResult{}, err
		}
		conflictKept, err := keepLocalTreeConflictIfChanged(localPath, event.Path, workspace, previousEntries)
		if err != nil {
			return pullApplyResult{}, err
		}
		if err := os.RemoveAll(localPath); err != nil {
			return pullApplyResult{}, err
		}
		return pullApplyResult{deleted: 1, conflictKept: boolToInt(conflictKept)}, nil
	case "move":
		if event.OldPath == nil {
			return pullApplyResult{}, errors.New("move event is missing old_path")
		}
		oldLocalPath, ok, err := localPathForRemote(root, workspace.RemotePath, *event.OldPath)
		if err != nil || !ok {
			return pullApplyResult{}, err
		}
		conflictKept, err := keepLocalTreeConflictIfChanged(oldLocalPath, *event.OldPath, workspace, previousEntries)
		if err != nil {
			return pullApplyResult{}, err
		}
		if conflictKept {
			if event.Version != nil {
				newLocalPath, ok, err := localPathForRemote(root, workspace.RemotePath, event.Path)
				if err != nil || !ok {
					return pullApplyResult{}, err
				}
				if err := downloadChangeFile(ctx, apiClient, accessToken, event.FileID, newLocalPath); err != nil {
					return pullApplyResult{}, err
				}
			}
			return pullApplyResult{moved: 1, conflictKept: 1}, nil
		}
		if err := moveLocalPath(root, workspace.RemotePath, *event.OldPath, event.Path); err != nil {
			return pullApplyResult{}, err
		}
		return pullApplyResult{moved: 1}, nil
	default:
		return pullApplyResult{}, fmt.Errorf("unsupported change event type: %s", event.EventType)
	}
}

func keepLocalConflictIfChanged(localPath, remotePath string, workspace workspaceConfig, previousEntries map[string]manifest.Entry) (bool, error) {
	previous, ok := previousEntries[remotePath]
	if !ok || previous.SHA256 == "" {
		return false, nil
	}
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	currentSHA, err := localFileSHA256(localPath)
	if err != nil {
		return false, err
	}
	if currentSHA == previous.SHA256 {
		return false, nil
	}
	conflictPath := conflictLocalPath(localPath, conflictDeviceLabel(workspace), syncPushNow().UTC())
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(localPath, conflictPath); err != nil {
		return false, err
	}
	return true, nil
}

func keepLocalTreeConflictIfChanged(localPath, remotePath string, workspace workspaceConfig, previousEntries map[string]manifest.Entry) (bool, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return keepLocalConflictIfChanged(localPath, remotePath, workspace, previousEntries)
	}
	changed, err := directoryHasLocalChanges(localPath, remotePath, previousEntries)
	if err != nil || !changed {
		return false, err
	}
	conflictPath := conflictLocalPath(localPath, conflictDeviceLabel(workspace), syncPushNow().UTC())
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(localPath, conflictPath); err != nil {
		return false, err
	}
	return true, nil
}

func directoryHasLocalChanges(localPath, remotePath string, previousEntries map[string]manifest.Entry) (bool, error) {
	remotePath, err := normalizeRemotePath(remotePath)
	if err != nil {
		return false, err
	}
	err = filepath.WalkDir(localPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errDirectoryChanged
		}
		relative, err := filepath.Rel(localPath, path)
		if err != nil {
			return err
		}
		childRemotePath := joinRemoteChild(remotePath, filepath.ToSlash(relative))
		previous, ok := previousEntries[childRemotePath]
		if !ok || previous.SHA256 == "" {
			return errDirectoryChanged
		}
		currentSHA, err := localFileSHA256(path)
		if err != nil {
			return err
		}
		if currentSHA != previous.SHA256 {
			return errDirectoryChanged
		}
		return nil
	})
	if errors.Is(err, errDirectoryChanged) {
		return true, nil
	}
	return false, err
}

var errDirectoryChanged = errors.New("directory has local changes")

func joinRemoteChild(remotePath, relative string) string {
	relative = strings.TrimPrefix(strings.ReplaceAll(relative, "\\", "/"), "/")
	if relative == "" {
		return remotePath
	}
	if remotePath == "/" {
		return "/" + relative
	}
	return pathpkg.Join(remotePath, relative)
}

func conflictLocalPath(localPath, device string, timestamp time.Time) string {
	dir := filepath.Dir(localPath)
	base := filepath.Base(localPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = strings.TrimPrefix(base, ".")
		ext = ""
	}
	conflictName := fmt.Sprintf("%s.conflict-%s-%s%s", name, sanitizeConflictPathPart(device), timestamp.UTC().Format("20060102T150405.000000000Z"), ext)
	return filepath.Join(dir, conflictName)
}

func localFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func moveLocalPath(root, remoteRoot, oldRemotePath, newRemotePath string) error {
	oldLocalPath, ok, err := localPathForRemote(root, remoteRoot, oldRemotePath)
	if err != nil || !ok {
		return err
	}
	newLocalPath, ok, err := localPathForRemote(root, remoteRoot, newRemotePath)
	if err != nil || !ok {
		return err
	}
	if _, err := os.Stat(oldLocalPath); err != nil {
		if os.IsNotExist(err) {
			if _, targetErr := os.Stat(newLocalPath); targetErr == nil {
				return nil
			} else if !os.IsNotExist(targetErr) {
				return targetErr
			}
		}
		return err
	}
	if _, err := os.Stat(newLocalPath); err == nil {
		return fmt.Errorf("move target already exists: %s", newLocalPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newLocalPath), 0o755); err != nil {
		return err
	}
	return os.Rename(oldLocalPath, newLocalPath)
}

func downloadChangeFile(ctx context.Context, apiClient *client.Client, accessToken, fileID, localPath string) error {
	result, err := apiClient.DownloadFile(ctx, accessToken, fileID, client.DownloadOptions{})
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode == http.StatusNotModified {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".synchub-pull-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, result.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, localPath); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

func localPathForRemote(root, remoteRoot, remotePath string) (string, bool, error) {
	remoteRoot, err := normalizeRemotePath(remoteRoot)
	if err != nil {
		return "", false, err
	}
	remotePath, err = normalizeRemotePath(remotePath)
	if err != nil {
		return "", false, err
	}
	var relative string
	switch {
	case remoteRoot == "/":
		relative = strings.TrimPrefix(remotePath, "/")
	case remotePath == remoteRoot:
		relative = ""
	case strings.HasPrefix(remotePath, remoteRoot+"/"):
		relative = strings.TrimPrefix(strings.TrimPrefix(remotePath, remoteRoot), "/")
	default:
		return "", false, nil
	}
	localPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := ensureLocalPathInsideRoot(root, localPath); err != nil {
		return "", false, err
	}
	return localPath, true, nil
}

func ensureLocalPathInsideRoot(root, localPath string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, absLocal)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return fmt.Errorf("remote path resolves outside workspace: %s", localPath)
	}
	return nil
}

func defaultDeviceName(root string) string {
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return hostname
	}
	name := filepath.Base(filepath.Clean(root))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "synchub-cli"
	}
	return name
}
