package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruceblink/SyncHub/pkg/client"
)

const (
	syncDoctorStatusOK      = "ok"
	syncDoctorStatusWarn    = "warn"
	syncDoctorStatusFail    = "fail"
	syncDoctorStatusSkipped = "skipped"
)

type syncDoctorReport struct {
	OK        bool                `json:"ok"`
	Workspace syncDoctorWorkspace `json:"workspace,omitempty"`
	ServerURL string              `json:"server_url,omitempty"`
	Checks    []syncDoctorCheck   `json:"checks"`
	Next      []string            `json:"next,omitempty"`
}

type syncDoctorWorkspace struct {
	Root                string `json:"root,omitempty"`
	ConfigPath          string `json:"config_path,omitempty"`
	RemotePath          string `json:"remote_path,omitempty"`
	UserEmail           string `json:"user_email,omitempty"`
	DeviceID            string `json:"device_id,omitempty"`
	DeviceName          string `json:"device_name,omitempty"`
	DevicePlatform      string `json:"device_platform,omitempty"`
	LastAppliedChangeID int64  `json:"last_applied_change_id,omitempty"`
}

type syncDoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func runSyncDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	manifestPath := fs.String("manifest", "", "manifest file path")
	jsonOutput := fs.Bool("json", false, "print doctor report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	report := newSyncDoctorReport()
	root, workspace, workspaceOK := runSyncDoctorWorkspaceCheck(&report, *rootPath, *workspaceConfigPath)
	loginConfig, loginOK := runSyncDoctorLoginCheck(&report, *configPath)
	serverURL := syncDoctorServerURL(workspace, workspaceOK, loginConfig, loginOK)
	report.ServerURL = serverURL

	serverReady := runSyncDoctorServerCheck(ctx, &report, serverURL)
	var devices []client.Device
	authOK := false
	if loginOK && serverReady {
		devices, authOK = runSyncDoctorAuthCheck(ctx, &report, serverURL, loginConfig)
	} else {
		report.add(syncDoctorStatusSkipped, "auth", "login config or server readiness check did not pass")
	}
	if workspaceOK {
		runSyncDoctorDeviceCheck(&report, workspace, devices, authOK)
		runSyncDoctorManifestCheck(ctx, &report, root, workspace, *manifestPath)
	}
	if strings.TrimSpace(root) != "" {
		runSyncDoctorAgentCheck(&report, root)
	}

	report.OK = !report.hasFailures()
	if *jsonOutput {
		if err := writeSyncDoctorJSON(stdout, report); err != nil {
			return err
		}
	} else {
		printSyncDoctorText(stdout, report)
	}
	if !report.OK {
		return errors.New("sync doctor found failing checks")
	}
	return nil
}

func newSyncDoctorReport() syncDoctorReport {
	return syncDoctorReport{
		OK:     true,
		Checks: []syncDoctorCheck{},
		Next:   []string{},
	}
}

func (r *syncDoctorReport) add(status, name, detail string) {
	r.Checks = append(r.Checks, syncDoctorCheck{Name: name, Status: status, Detail: detail})
}

func (r *syncDoctorReport) addNext(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	for _, existing := range r.Next {
		if existing == command {
			return
		}
	}
	r.Next = append(r.Next, command)
}

func (r syncDoctorReport) hasFailures() bool {
	for _, check := range r.Checks {
		if check.Status == syncDoctorStatusFail {
			return true
		}
	}
	return false
}

func runSyncDoctorWorkspaceCheck(report *syncDoctorReport, rootPath, workspaceConfigPath string) (string, workspaceConfig, bool) {
	root, err := resolveWorkspaceRoot(rootPath)
	if err != nil {
		report.add(syncDoctorStatusFail, "workspace root", err.Error())
		report.addNext("create the workspace directory and rerun sync doctor")
		return "", workspaceConfig{}, false
	}
	report.add(syncDoctorStatusOK, "workspace root", root)

	configPath := workspaceConfigPath
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, ".synchub", "workspace.json")
	}
	workspace, err := readWorkspaceConfig(configPath)
	if err != nil {
		report.add(syncDoctorStatusFail, "workspace config", err.Error())
		report.addNext("synchub-cli workspace init --path . --remote-path /workspace")
		return root, workspaceConfig{}, false
	}
	if strings.TrimSpace(workspace.Root) != "" {
		root = workspace.Root
	}
	report.Workspace = syncDoctorWorkspace{
		Root:                root,
		ConfigPath:          configPath,
		RemotePath:          workspace.RemotePath,
		UserEmail:           workspace.UserEmail,
		DeviceID:            workspace.DeviceID,
		DeviceName:          workspace.DeviceName,
		DevicePlatform:      workspace.DevicePlatform,
		LastAppliedChangeID: workspace.LastAppliedChangeID,
	}
	report.add(syncDoctorStatusOK, "workspace config", fmt.Sprintf("%s remote=%s user=%s", configPath, workspace.RemotePath, workspace.UserEmail))
	return root, workspace, true
}

func runSyncDoctorLoginCheck(report *syncDoctorReport, configPath string) (cliConfig, bool) {
	cfg, err := readConfig(configPath)
	if err != nil {
		report.add(syncDoctorStatusFail, "login config", err.Error())
		report.addNext("synchub-cli login --server http://localhost:8765 --email <email> --password <password>")
		return cliConfig{}, false
	}
	detail := fmt.Sprintf("%s user=%s server=%s", configPath, cfg.User.Email, cfg.ServerURL)
	if shouldRefreshAccessToken(cfg, time.Now().UTC()) {
		report.add(syncDoctorStatusWarn, "login config", detail+"; access token is near expiry")
		report.addNext("synchub-cli login --server " + cfg.ServerURL + " --email <email> --password <password>")
		return cfg, true
	}
	report.add(syncDoctorStatusOK, "login config", detail)
	return cfg, true
}

func syncDoctorServerURL(workspace workspaceConfig, workspaceOK bool, loginConfig cliConfig, loginOK bool) string {
	if workspaceOK && strings.TrimSpace(workspace.ServerURL) != "" {
		return workspace.ServerURL
	}
	if loginOK && strings.TrimSpace(loginConfig.ServerURL) != "" {
		return loginConfig.ServerURL
	}
	return defaultServerURL
}

func runSyncDoctorServerCheck(ctx context.Context, report *syncDoctorReport, serverURL string) bool {
	ready, err := client.New(serverURL).Ready(ctx)
	if err != nil {
		report.add(syncDoctorStatusFail, "server ready", fmt.Sprintf("%s: %v", serverURL, err))
		report.addNext("go run ./cmd/synchub-api")
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(ready.Status), "ready") {
		report.add(syncDoctorStatusFail, "server ready", fmt.Sprintf("%s returned status %s", serverURL, ready.Status))
		report.addNext("go run ./cmd/synchub-api")
		return false
	}
	report.add(syncDoctorStatusOK, "server ready", serverURL)
	return true
}

func runSyncDoctorAuthCheck(ctx context.Context, report *syncDoctorReport, serverURL string, loginConfig cliConfig) ([]client.Device, bool) {
	devices, err := client.New(serverURL).ListDevices(ctx, loginConfig.Tokens.AccessToken, 100)
	if err != nil {
		report.add(syncDoctorStatusFail, "auth", err.Error())
		report.addNext("synchub-cli login --server " + serverURL + " --email <email> --password <password>")
		return nil, false
	}
	report.add(syncDoctorStatusOK, "auth", fmt.Sprintf("token accepted; devices=%d", len(devices.Items)))
	return devices.Items, true
}

func runSyncDoctorDeviceCheck(report *syncDoctorReport, workspace workspaceConfig, devices []client.Device, authOK bool) {
	if strings.TrimSpace(workspace.DeviceID) == "" {
		report.add(syncDoctorStatusWarn, "device", "workspace device is not registered")
		report.addNext("synchub-cli sync once --path .")
		return
	}
	if !authOK {
		report.add(syncDoctorStatusSkipped, "device", "auth check did not pass")
		return
	}
	for _, device := range devices {
		if device.ID == workspace.DeviceID {
			report.add(syncDoctorStatusOK, "device", fmt.Sprintf("%s name=%s platform=%s cursor=%d", device.ID, device.Name, device.Platform, device.LastAppliedChangeID))
			return
		}
	}
	report.add(syncDoctorStatusFail, "device", fmt.Sprintf("workspace device %s was not found on server", workspace.DeviceID))
	report.addNext("synchub-cli sync once --path .")
}

func runSyncDoctorManifestCheck(ctx context.Context, report *syncDoctorReport, root string, workspace workspaceConfig, manifestPath string) {
	localManifestPath := manifestPath
	if strings.TrimSpace(localManifestPath) == "" {
		localManifestPath = filepath.Join(root, ".synchub", "manifest.json")
	}
	m, err := readManifest(localManifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.add(syncDoctorStatusWarn, "manifest", fmt.Sprintf("%s is missing", localManifestPath))
			report.addNext("synchub-cli sync once --path .")
			return
		}
		report.add(syncDoctorStatusFail, "manifest", err.Error())
		return
	}
	if m.Root != "" && filepath.Clean(m.Root) != filepath.Clean(root) {
		report.add(syncDoctorStatusFail, "manifest", fmt.Sprintf("manifest root %s does not match workspace root %s", m.Root, root))
		return
	}
	changes, err := scanManifestChanges(ctx, root, workspace.RemotePath, localManifestPath)
	if err != nil {
		report.add(syncDoctorStatusFail, "manifest", err.Error())
		return
	}
	detail := fmt.Sprintf("%s files=%d pending=%d", localManifestPath, len(m.Items), len(changes))
	if len(changes) > 0 {
		report.add(syncDoctorStatusWarn, "manifest", detail)
		report.addNext("synchub-cli sync once --path .")
		return
	}
	report.add(syncDoctorStatusOK, "manifest", detail)
}

func runSyncDoctorAgentCheck(report *syncDoctorReport, root string) {
	control, controlOK, err := readSyncAgentControl(root)
	if err != nil {
		report.add(syncDoctorStatusFail, "daemon", err.Error())
		return
	}
	state, stateOK, err := readSyncAgentState(root)
	if err != nil {
		report.add(syncDoctorStatusFail, "daemon", err.Error())
		return
	}
	if controlOK && control.Paused {
		report.add(syncDoctorStatusWarn, "daemon", "daemon is paused")
		report.addNext("synchub-cli sync daemon --path . --resume")
		return
	}
	if stateOK && strings.EqualFold(strings.TrimSpace(state.Status), "error") {
		report.add(syncDoctorStatusWarn, "daemon", "last run failed: "+state.LastError)
		return
	}
	if stateOK {
		report.add(syncDoctorStatusOK, "daemon", "last status: "+state.Status)
		return
	}
	report.add(syncDoctorStatusOK, "daemon", "not paused")
}

func writeSyncDoctorJSON(stdout io.Writer, report syncDoctorReport) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func printSyncDoctorText(stdout io.Writer, report syncDoctorReport) {
	if report.OK {
		fmt.Fprintln(stdout, "sync doctor: ok")
	} else {
		fmt.Fprintln(stdout, "sync doctor: failed")
	}
	if strings.TrimSpace(report.ServerURL) != "" {
		fmt.Fprintf(stdout, "server: %s\n", report.ServerURL)
	}
	for _, check := range report.Checks {
		if strings.TrimSpace(check.Detail) == "" {
			fmt.Fprintf(stdout, "[%s] %s\n", check.Status, check.Name)
			continue
		}
		fmt.Fprintf(stdout, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
	}
	if len(report.Next) > 0 {
		fmt.Fprintln(stdout, "next:")
		for _, command := range report.Next {
			fmt.Fprintf(stdout, "  %s\n", command)
		}
	}
}
