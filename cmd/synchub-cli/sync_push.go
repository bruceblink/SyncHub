package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
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

type pushActionResult struct {
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
	deviceName := fs.String("device-name", "", "device name")
	devicePlatform := fs.String("platform", runtime.GOOS, "device platform")
	dryRun := fs.Bool("dry-run", false, "preview local push changes without contacting the server")
	jsonOutput := fs.Bool("json", false, "print push result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root, workspace, workspacePath, err := loadWorkspace(*rootPath, *workspaceConfigPath)
	if err != nil {
		return err
	}
	localManifestPath := *manifestPath
	if strings.TrimSpace(localManifestPath) == "" {
		localManifestPath = filepath.Join(root, ".synchub", "manifest.json")
	}
	manifestMissing := false
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			manifestMissing = true
			m = manifest.Manifest{
				Version:    1,
				Root:       root,
				RemotePath: workspace.RemotePath,
			}
		} else {
			return err
		}
	}
	if m.Root != "" && filepath.Clean(m.Root) != filepath.Clean(root) {
		return fmt.Errorf("manifest root %s does not match workspace root %s", m.Root, root)
	}
	currentManifest, err := manifest.Scan(ctx, root, workspace.RemotePath)
	if err != nil {
		return err
	}
	if err := mergePushManifestRemoteVersions(&currentManifest, m); err != nil {
		return err
	}
	previousEntries, err := manifestEntriesByPath(m)
	if err != nil {
		return err
	}
	currentPaths, err := manifestPathSet(currentManifest)
	if err != nil {
		return err
	}
	plannedMoves, err := planPushMoves(m, currentManifest, currentPaths)
	if err != nil {
		return err
	}
	if *dryRun {
		plan, err := planPushPreview(m, currentManifest, currentPaths, previousEntries, plannedMoves)
		if err != nil {
			return err
		}
		if *jsonOutput {
			return writePushPreviewJSON(stdout, workspace, plan)
		}
		printPushPreview(stdout, plan)
		return nil
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

	createdDirs := map[string]struct{}{}
	uploaded := 0
	deleted := 0
	moved := 0
	conflictKept := 0
	uploadItems := []syncPushUploadItem{}
	deleteItems := []syncPushDeleteItem{}
	moveItems := []syncPushMoveItem{}
	manifestChanged := manifestMissing
	hasPushChanges := len(plannedMoves) > 0 || hasDeletedManifestEntries(m, currentPaths)
	if !hasPushChanges {
		for _, item := range currentManifest.Items {
			path, err := normalizeRemotePath(item.Path)
			if err != nil {
				return err
			}
			previousItem, existed := previousEntries[path]
			if !existed || previousItem.RemoteVersion == nil || manifestContentChanged(previousItem, item) {
				hasPushChanges = true
				break
			}
		}
	}
	if hasPushChanges && strings.TrimSpace(workspace.DeviceID) == "" {
		if err := ensureWorkspacePushDevice(ctx, apiClient, loginConfig.Tokens.AccessToken, root, workspacePath, &workspace, *deviceName, *devicePlatform); err != nil {
			return err
		}
	}
	moveSources := make(map[string]struct{}, len(plannedMoves))
	moveTargets := make(map[string]int64, len(plannedMoves))
	moveConflictTargets := make(map[string]struct{}, len(plannedMoves))
	for _, move := range plannedMoves {
		if err := ensureRemoteDirectories(ctx, apiClient, loginConfig.Tokens.AccessToken, workspace.DeviceID, move.to.Path, createdDirs); err != nil {
			return err
		}
		result, err := moveManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, workspace.DeviceID, move.from, move.to)
		if err != nil {
			return err
		}
		if result.conflictKept {
			moveItems = append(moveItems, syncPushMoveItemFromPlan(move, 0, true))
			moveSources[move.sourcePath] = struct{}{}
			moveConflictTargets[move.targetPath] = struct{}{}
			conflictKept++
			continue
		}
		moveItems = append(moveItems, syncPushMoveItemFromPlan(move, result.version, false))
		moveSources[move.sourcePath] = struct{}{}
		if result.version > 0 {
			moveTargets[move.targetPath] = result.version
		}
		moved++
		manifestChanged = true
	}
	for _, item := range m.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return err
		}
		if _, ok := moveSources[path]; ok {
			continue
		}
		if _, ok := currentPaths[path]; !ok {
			result, err := deleteManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, workspace.DeviceID, item)
			if err != nil {
				return err
			}
			if result.conflictKept {
				deleteItems = append(deleteItems, syncPushDeleteItemFromEntry(item, true))
				conflictKept++
				continue
			}
			deleteItems = append(deleteItems, syncPushDeleteItemFromEntry(item, false))
			deleted++
			manifestChanged = true
		}
	}
	for i, item := range currentManifest.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return err
		}
		if version, ok := moveTargets[path]; ok {
			version := version
			currentManifest.Items[i].RemoteVersion = &version
			continue
		}
		if _, ok := moveConflictTargets[path]; ok {
			continue
		}
		previousItem, existed := previousEntries[path]
		if existed && previousItem.RemoteVersion != nil && !manifestContentChanged(previousItem, item) {
			continue
		}
		action := "create"
		if existed && previousItem.RemoteVersion != nil {
			action = "update"
		}
		result, err := pushManifestEntry(ctx, apiClient, loginConfig.Tokens.AccessToken, root, workspace, item, createdDirs)
		if err != nil {
			return err
		}
		uploadItems = append(uploadItems, syncPushUploadItemFromEntry(action, item, result.version, result.conflictKept))
		uploaded++
		if result.version > 0 && (item.RemoteVersion == nil || *item.RemoteVersion != result.version) {
			version := result.version
			currentManifest.Items[i].RemoteVersion = &version
			manifestChanged = true
		}
		if result.conflictKept {
			conflictKept++
		}
	}
	if manifestChanged {
		if err := writeManifest(localManifestPath, currentManifest); err != nil {
			return err
		}
	}
	if *jsonOutput {
		return writeSyncPushJSON(stdout, workspace, false, syncPushSummary{
			Changes:       len(uploadItems) + len(deleteItems) + len(moveItems),
			Uploaded:      uploaded,
			Deleted:       deleted,
			Moved:         moved,
			ConflictsKept: conflictKept,
		}, uploadItems, deleteItems, moveItems)
	}
	fmt.Fprintf(stdout, "uploaded: %d files\n", uploaded)
	if deleted > 0 {
		fmt.Fprintf(stdout, "deleted: %d files\n", deleted)
	}
	if moved > 0 {
		fmt.Fprintf(stdout, "moved: %d files\n", moved)
	}
	if conflictKept > 0 {
		fmt.Fprintf(stdout, "conflicts kept: %d\n", conflictKept)
	}
	return nil
}

type syncPushSnapshot struct {
	Workspace syncPushWorkspace    `json:"workspace"`
	DryRun    bool                 `json:"dry_run"`
	Summary   syncPushSummary      `json:"summary"`
	Uploads   []syncPushUploadItem `json:"uploads"`
	Deletes   []syncPushDeleteItem `json:"deletes"`
	Moves     []syncPushMoveItem   `json:"moves"`
}

type syncPushWorkspace struct {
	Root       string `json:"root"`
	RemotePath string `json:"remote_path"`
	UserEmail  string `json:"user_email"`
	DeviceID   string `json:"device_id,omitempty"`
}

type syncPushSummary struct {
	Changes       int `json:"changes"`
	Uploaded      int `json:"uploaded"`
	Deleted       int `json:"deleted"`
	Moved         int `json:"moved"`
	ConflictsKept int `json:"conflicts_kept"`
}

type syncPushUploadItem struct {
	Action       string `json:"action"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	BaseVersion  *int64 `json:"base_version,omitempty"`
	Version      int64  `json:"version,omitempty"`
	ConflictKept bool   `json:"conflict_kept,omitempty"`
}

type syncPushDeleteItem struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	BaseVersion  *int64 `json:"base_version,omitempty"`
	ConflictKept bool   `json:"conflict_kept,omitempty"`
}

type syncPushMoveItem struct {
	From         string `json:"from"`
	To           string `json:"to"`
	RelativeFrom string `json:"relative_from"`
	RelativeTo   string `json:"relative_to"`
	BaseVersion  *int64 `json:"base_version,omitempty"`
	Version      int64  `json:"version,omitempty"`
	ConflictKept bool   `json:"conflict_kept,omitempty"`
}

func writePushPreviewJSON(stdout io.Writer, workspace workspaceConfig, plan pushPreviewPlan) error {
	uploads := make([]syncPushUploadItem, 0, len(plan.uploads))
	for _, upload := range plan.uploads {
		uploads = append(uploads, syncPushUploadItemFromEntry(upload.action, upload.item, 0, false))
	}
	deletes := make([]syncPushDeleteItem, 0, len(plan.deletes))
	for _, item := range plan.deletes {
		deletes = append(deletes, syncPushDeleteItemFromEntry(item, false))
	}
	moves := make([]syncPushMoveItem, 0, len(plan.moves))
	for _, move := range plan.moves {
		moves = append(moves, syncPushMoveItemFromPlan(move, 0, false))
	}
	return writeSyncPushJSON(stdout, workspace, true, syncPushSummary{
		Changes:  len(uploads) + len(deletes) + len(moves),
		Uploaded: len(uploads),
		Deleted:  len(deletes),
		Moved:    len(moves),
	}, uploads, deletes, moves)
}

func writeSyncPushJSON(stdout io.Writer, workspace workspaceConfig, dryRun bool, summary syncPushSummary, uploads []syncPushUploadItem, deletes []syncPushDeleteItem, moves []syncPushMoveItem) error {
	if uploads == nil {
		uploads = []syncPushUploadItem{}
	}
	if deletes == nil {
		deletes = []syncPushDeleteItem{}
	}
	if moves == nil {
		moves = []syncPushMoveItem{}
	}
	snapshot := syncPushSnapshot{
		Workspace: syncPushWorkspace{
			Root:       workspace.Root,
			RemotePath: workspace.RemotePath,
			UserEmail:  workspace.UserEmail,
			DeviceID:   workspace.DeviceID,
		},
		DryRun:  dryRun,
		Summary: summary,
		Uploads: uploads,
		Deletes: deletes,
		Moves:   moves,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func syncPushUploadItemFromEntry(action string, item manifest.Entry, version int64, conflictKept bool) syncPushUploadItem {
	return syncPushUploadItem{
		Action:       action,
		Path:         item.Path,
		RelativePath: item.RelativePath,
		Size:         item.Size,
		SHA256:       item.SHA256,
		BaseVersion:  item.RemoteVersion,
		Version:      version,
		ConflictKept: conflictKept,
	}
}

func syncPushDeleteItemFromEntry(item manifest.Entry, conflictKept bool) syncPushDeleteItem {
	return syncPushDeleteItem{
		Path:         item.Path,
		RelativePath: item.RelativePath,
		BaseVersion:  item.RemoteVersion,
		ConflictKept: conflictKept,
	}
}

func syncPushMoveItemFromPlan(move pushMovePlan, version int64, conflictKept bool) syncPushMoveItem {
	return syncPushMoveItem{
		From:         move.sourcePath,
		To:           move.targetPath,
		RelativeFrom: move.from.RelativePath,
		RelativeTo:   move.to.RelativePath,
		BaseVersion:  move.from.RemoteVersion,
		Version:      version,
		ConflictKept: conflictKept,
	}
}

type pushPreviewPlan struct {
	uploads []pushUploadPreview
	deletes []manifest.Entry
	moves   []pushMovePlan
}

type pushUploadPreview struct {
	action string
	item   manifest.Entry
}

func planPushPreview(previous, current manifest.Manifest, currentPaths map[string]struct{}, previousEntries map[string]manifest.Entry, plannedMoves []pushMovePlan) (pushPreviewPlan, error) {
	plan := pushPreviewPlan{moves: plannedMoves}
	moveSources := make(map[string]struct{}, len(plannedMoves))
	moveTargets := make(map[string]struct{}, len(plannedMoves))
	for _, move := range plannedMoves {
		moveSources[move.sourcePath] = struct{}{}
		moveTargets[move.targetPath] = struct{}{}
	}
	for _, item := range previous.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return pushPreviewPlan{}, err
		}
		if _, ok := moveSources[path]; ok {
			continue
		}
		if _, ok := currentPaths[path]; !ok {
			plan.deletes = append(plan.deletes, item)
		}
	}
	for _, item := range current.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return pushPreviewPlan{}, err
		}
		if _, ok := moveTargets[path]; ok {
			continue
		}
		previousItem, existed := previousEntries[path]
		if existed && previousItem.RemoteVersion != nil && !manifestContentChanged(previousItem, item) {
			continue
		}
		action := "create"
		if existed && previousItem.RemoteVersion != nil {
			action = "update"
		}
		plan.uploads = append(plan.uploads, pushUploadPreview{action: action, item: item})
	}
	return plan, nil
}

func printPushPreview(stdout io.Writer, plan pushPreviewPlan) {
	fmt.Fprintln(stdout, "dry run: true")
	fmt.Fprintf(stdout, "changes: %d\n", len(plan.uploads)+len(plan.deletes)+len(plan.moves))
	for _, move := range plan.moves {
		fmt.Fprintf(stdout, "move %s -> %s base_version=%s\n", move.sourcePath, move.targetPath, versionString(move.from.RemoteVersion))
	}
	for _, item := range plan.deletes {
		fmt.Fprintf(stdout, "delete %s base_version=%s\n", item.Path, versionString(item.RemoteVersion))
	}
	for _, upload := range plan.uploads {
		fmt.Fprintf(stdout, "%s %s size=%d base_version=%s\n", upload.action, upload.item.Path, upload.item.Size, versionString(upload.item.RemoteVersion))
	}
	fmt.Fprintf(stdout, "uploaded: %d files\n", len(plan.uploads))
	fmt.Fprintf(stdout, "deleted: %d files\n", len(plan.deletes))
	fmt.Fprintf(stdout, "moved: %d files\n", len(plan.moves))
}

func ensureWorkspacePushDevice(ctx context.Context, apiClient *client.Client, accessToken, root, workspacePath string, workspace *workspaceConfig, deviceName, platform string) error {
	if strings.TrimSpace(workspace.DeviceID) != "" {
		return nil
	}
	changed, err := ensureWorkspaceDevice(ctx, apiClient, accessToken, root, workspace, deviceName, platform)
	if err != nil {
		return err
	}
	if changed {
		return writeWorkspaceConfig(workspacePath, *workspace)
	}
	return nil
}

func pushManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, root string, workspace workspaceConfig, item manifest.Entry, createdDirs map[string]struct{}) (pushManifestResult, error) {
	version, err := uploadManifestEntry(ctx, apiClient, accessToken, root, workspace, item, createdDirs)
	if err != nil {
		if !isAPIErrorCode(err, "FILE_CONFLICT") {
			return pushManifestResult{}, err
		}
		conflictItem := item
		conflictItem.Path = conflictRemotePath(item.Path, conflictDeviceLabel(workspace), syncPushNow().UTC())
		conflictItem.RemoteVersion = nil
		if _, err := uploadManifestEntry(ctx, apiClient, accessToken, root, workspace, conflictItem, createdDirs); err != nil {
			return pushManifestResult{}, fmt.Errorf("upload conflict copy for %s: %w", item.Path, err)
		}
		return pushManifestResult{conflictKept: true}, nil
	}
	return pushManifestResult{version: version}, nil
}

type pushMovePlan struct {
	sourcePath string
	targetPath string
	from       manifest.Entry
	to         manifest.Entry
}

func planPushMoves(previous manifest.Manifest, current manifest.Manifest, currentPaths map[string]struct{}) ([]pushMovePlan, error) {
	type key struct {
		sha256 string
		size   int64
	}
	candidates := map[key][]manifest.Entry{}
	for _, item := range current.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return nil, err
		}
		if previousHasPath(previous, path) {
			continue
		}
		candidates[key{sha256: item.SHA256, size: item.Size}] = append(candidates[key{sha256: item.SHA256, size: item.Size}], item)
	}
	moves := []pushMovePlan{}
	for _, item := range previous.Items {
		sourcePath, err := normalizeRemotePath(item.Path)
		if err != nil {
			return nil, err
		}
		if _, ok := currentPaths[sourcePath]; ok {
			continue
		}
		if item.RemoteVersion == nil {
			continue
		}
		k := key{sha256: item.SHA256, size: item.Size}
		items := candidates[k]
		if len(items) != 1 {
			continue
		}
		targetPath, err := normalizeRemotePath(items[0].Path)
		if err != nil {
			return nil, err
		}
		moves = append(moves, pushMovePlan{sourcePath: sourcePath, targetPath: targetPath, from: item, to: items[0]})
		delete(candidates, k)
	}
	return moves, nil
}

func previousHasPath(previous manifest.Manifest, remotePath string) bool {
	for _, item := range previous.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			continue
		}
		if path == remotePath {
			return true
		}
	}
	return false
}

func manifestEntriesByPath(m manifest.Manifest) (map[string]manifest.Entry, error) {
	entries := make(map[string]manifest.Entry, len(m.Items))
	for _, item := range m.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return nil, err
		}
		entries[path] = item
	}
	return entries, nil
}

func manifestContentChanged(before, after manifest.Entry) bool {
	return before.Size != after.Size || before.SHA256 != after.SHA256
}

func hasDeletedManifestEntries(previous manifest.Manifest, currentPaths map[string]struct{}) bool {
	for _, item := range previous.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			continue
		}
		if _, ok := currentPaths[path]; !ok {
			return true
		}
	}
	return false
}

func moveManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, deviceID string, from, to manifest.Entry) (pushActionResult, error) {
	node, err := apiClient.GetFileByPath(ctx, accessToken, from.Path)
	if err != nil {
		return pushActionResult{}, err
	}
	moved, err := apiClient.MoveFileWithDeviceAndBaseVersion(ctx, accessToken, node.ID, to.Path, deviceID, from.RemoteVersion)
	if err != nil {
		if isAPIErrorCode(err, "FILE_CONFLICT") {
			return pushActionResult{conflictKept: true}, nil
		}
		return pushActionResult{}, err
	}
	return pushActionResult{version: moved.Version}, nil
}

func mergePushManifestRemoteVersions(current *manifest.Manifest, previous manifest.Manifest) error {
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

func manifestPathSet(m manifest.Manifest) (map[string]struct{}, error) {
	paths := make(map[string]struct{}, len(m.Items))
	for _, item := range m.Items {
		path, err := normalizeRemotePath(item.Path)
		if err != nil {
			return nil, err
		}
		paths[path] = struct{}{}
	}
	return paths, nil
}

func deleteManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, deviceID string, item manifest.Entry) (pushActionResult, error) {
	node, err := apiClient.GetFileByPath(ctx, accessToken, item.Path)
	if err != nil {
		if isAPIErrorCode(err, "NOT_FOUND") {
			return pushActionResult{}, nil
		}
		return pushActionResult{}, err
	}
	if err := apiClient.DeleteFileWithDeviceAndBaseVersion(ctx, accessToken, node.ID, deviceID, item.RemoteVersion); err != nil && !isAPIErrorCode(err, "NOT_FOUND") {
		if isAPIErrorCode(err, "FILE_CONFLICT") {
			return pushActionResult{conflictKept: true}, nil
		}
		return pushActionResult{}, err
	}
	return pushActionResult{}, nil
}

func uploadManifestEntry(ctx context.Context, apiClient *client.Client, accessToken, root string, workspace workspaceConfig, item manifest.Entry, createdDirs map[string]struct{}) (int64, error) {
	localPath := filepath.Join(root, filepath.FromSlash(item.RelativePath))
	if err := ensureRemoteDirectories(ctx, apiClient, accessToken, workspace.DeviceID, item.Path, createdDirs); err != nil {
		return 0, err
	}
	session, err := apiClient.InitUpload(ctx, accessToken, client.InitUploadRequest{
		Path:        item.Path,
		Size:        item.Size,
		SHA256:      item.SHA256,
		BaseVersion: item.RemoteVersion,
		DeviceID:    workspace.DeviceID,
	}, uploadIdempotencyKey(item))
	if err != nil {
		return 0, err
	}
	if err := uploadFileChunks(ctx, apiClient, accessToken, session.UploadID, localPath, session.ChunkSize, session.UploadedChunks); err != nil {
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

func ensureRemoteDirectories(ctx context.Context, apiClient *client.Client, accessToken, deviceID, filePath string, created map[string]struct{}) error {
	for _, dir := range remoteParentDirs(filePath) {
		if _, ok := created[dir]; ok {
			continue
		}
		if _, err := apiClient.CreateDirectoryWithDevice(ctx, accessToken, dir, deviceID); err != nil && !isAPIErrorCode(err, "ALREADY_EXISTS") {
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

func uploadFileChunks(ctx context.Context, apiClient *client.Client, accessToken, uploadID, localPath string, chunkSize int64, uploadedChunks []client.UploadChunk) error {
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
	uploaded := uploadedChunksByIndex(uploadedChunks)
	if info.Size() == 0 {
		return uploadChunkIfMissing(ctx, apiClient, accessToken, uploadID, 0, nil, uploaded)
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
		if err := uploadChunkIfMissing(ctx, apiClient, accessToken, uploadID, index, buf[:n], uploaded); err != nil {
			return err
		}
		index++
		if readErr == io.ErrUnexpectedEOF {
			return nil
		}
	}
}

func uploadedChunksByIndex(chunks []client.UploadChunk) map[int32]client.UploadChunk {
	uploaded := make(map[int32]client.UploadChunk, len(chunks))
	for _, chunk := range chunks {
		uploaded[chunk.ChunkIndex] = chunk
	}
	return uploaded
}

func uploadChunkIfMissing(ctx context.Context, apiClient *client.Client, accessToken, uploadID string, index int32, data []byte, uploaded map[int32]client.UploadChunk) error {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if chunk, ok := uploaded[index]; ok && chunk.Size == int32(len(data)) && strings.EqualFold(chunk.SHA256, sha) {
		return nil
	}
	_, err := apiClient.PutUploadChunk(ctx, accessToken, uploadID, index, bytes.NewReader(data), sha)
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
