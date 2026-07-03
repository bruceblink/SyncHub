package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const defaultAgentInterval = 30 * time.Second

var agentNow = time.Now

type agentOptions struct {
	RootPath            string
	ConfigPath          string
	WorkspaceConfigPath string
	ManifestPath        string
	CLIPath             string
	Interval            time.Duration
	DeviceName          string
	DevicePlatform      string
	Limit               int
	Once                bool
}

type syncOnceRunner func(context.Context, agentOptions, io.Writer, io.Writer) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, runSyncOnceCommand); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, runner syncOnceRunner) error {
	opts, err := parseOptions(args, stdout, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if runner == nil {
		return errors.New("sync runner is required")
	}
	if opts.Once {
		return runner(ctx, opts, stdout, stderr)
	}

	fmt.Fprintf(stdout, "agent started: %s\n", opts.RootPath)
	fmt.Fprintf(stdout, "sync interval: %s\n", opts.Interval)
	runSyncCycle(ctx, opts, stdout, stderr, runner)

	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runSyncCycle(ctx, opts, stdout, stderr, runner)
		}
	}
}

func runSyncCycle(ctx context.Context, opts agentOptions, stdout, stderr io.Writer, runner syncOnceRunner) {
	if err := runner(ctx, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "sync failed: %v\n", err)
		return
	}
	fmt.Fprintf(stdout, "sync completed: %s\n", agentNow().UTC().Format(time.RFC3339))
}

func parseOptions(args []string, stdout, stderr io.Writer) (agentOptions, error) {
	fs := flag.NewFlagSet("synchub-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		printUsage(stderr)
	}
	opts := agentOptions{}
	fs.StringVar(&opts.RootPath, "path", ".", "local workspace root")
	fs.StringVar(&opts.ConfigPath, "config", defaultConfigPath(), "login config file path")
	fs.StringVar(&opts.WorkspaceConfigPath, "workspace-config", "", "workspace config file path")
	fs.StringVar(&opts.ManifestPath, "manifest", "", "manifest file path")
	fs.StringVar(&opts.CLIPath, "cli", "", "synchub-cli executable path")
	fs.DurationVar(&opts.Interval, "interval", defaultAgentInterval, "sync interval")
	fs.StringVar(&opts.DeviceName, "device-name", "", "device name")
	fs.StringVar(&opts.DevicePlatform, "platform", "", "device platform")
	fs.IntVar(&opts.Limit, "limit", 500, "maximum changes to pull per sync cycle")
	fs.BoolVar(&opts.Once, "once", false, "run one sync cycle and exit")
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			printUsage(stdout)
			return agentOptions{}, flag.ErrHelp
		}
	}
	if err := fs.Parse(args); err != nil {
		return agentOptions{}, err
	}
	if opts.Interval <= 0 {
		return agentOptions{}, errors.New("sync interval must be positive")
	}
	if opts.Limit <= 0 {
		return agentOptions{}, errors.New("limit must be positive")
	}
	if strings.TrimSpace(opts.RootPath) == "" {
		return agentOptions{}, errors.New("workspace path is required")
	}
	return opts, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  synchub-agent --path .")
	fmt.Fprintln(w, "  synchub-agent --path . --once")
	fmt.Fprintln(w, "  synchub-agent --path . --interval 30s --device-name laptop --platform windows --limit 500")
}

func runSyncOnceCommand(ctx context.Context, opts agentOptions, stdout, stderr io.Writer) error {
	name, baseArgs := syncCommand(opts.CLIPath)
	args := append(baseArgs, buildSyncOnceArgs(opts)...)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func syncCommand(cliPath string) (string, []string) {
	if cliPath = strings.TrimSpace(cliPath); cliPath != "" {
		return cliPath, nil
	}
	if cliPath = strings.TrimSpace(os.Getenv("SYNCHUB_CLI")); cliPath != "" {
		return cliPath, nil
	}
	if path, err := exec.LookPath("synchub-cli"); err == nil {
		return path, nil
	}
	if _, err := os.Stat(filepath.Join("cmd", "synchub-cli")); err == nil {
		return "go", []string{"run", "./cmd/synchub-cli"}
	}
	return "synchub-cli", nil
}

func buildSyncOnceArgs(opts agentOptions) []string {
	args := []string{
		"sync",
		"once",
		"--path",
		opts.RootPath,
		"--config",
		opts.ConfigPath,
	}
	if strings.TrimSpace(opts.WorkspaceConfigPath) != "" {
		args = append(args, "--workspace-config", opts.WorkspaceConfigPath)
	}
	if strings.TrimSpace(opts.ManifestPath) != "" {
		args = append(args, "--manifest", opts.ManifestPath)
	}
	if strings.TrimSpace(opts.DeviceName) != "" {
		args = append(args, "--device-name", opts.DeviceName)
	}
	if strings.TrimSpace(opts.DevicePlatform) != "" {
		args = append(args, "--platform", opts.DevicePlatform)
	}
	args = append(args, "--limit", fmt.Sprintf("%d", opts.Limit))
	return args
}

func defaultConfigPath() string {
	if v := os.Getenv("SYNCHUB_CONFIG"); v != "" {
		return v
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(".synchub", "config.json")
	}
	return filepath.Join(dir, "SyncHub", "config.json")
}
