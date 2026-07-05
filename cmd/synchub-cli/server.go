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

func runServer(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printServerUsage(stderr)
		return errors.New("server command is required")
	}
	switch args[0] {
	case "status":
		return runServerStatus(ctx, args[1:], stdout, stderr)
	case "wait":
		return runServerWait(ctx, args[1:], stdout, stderr)
	case "metrics":
		return runServerMetrics(ctx, args[1:], stdout, stderr)
	case "openapi":
		return runServerOpenAPI(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printServerUsage(stdout)
		return nil
	default:
		printServerUsage(stderr)
		return fmt.Errorf("unknown server command: %s", args[0])
	}
}

func runServerStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", defaultServerURL, "server base URL")
	jsonOutput := fs.Bool("json", false, "print server status as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" {
		return errors.New("server URL is required")
	}

	api := client.New(*serverURL)
	version, err := api.Version(ctx)
	if err != nil {
		return fmt.Errorf("version check failed: %w", err)
	}
	health, err := api.Health(ctx)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	ready, err := api.Ready(ctx)
	if err != nil {
		return fmt.Errorf("readiness check failed: %w", err)
	}

	if *jsonOutput {
		return writeServerStatusJSON(stdout, api.BaseURL, version, health, ready)
	}
	fmt.Fprintf(stdout, "server: %s\n", api.BaseURL)
	fmt.Fprintf(stdout, "version: %s %s\n", version.Name, version.Version)
	fmt.Fprintf(stdout, "health: %s\n", health.Status)
	fmt.Fprintf(stdout, "ready: %s\n", ready.Status)
	return nil
}

type serverStatusSnapshot struct {
	Server  string             `json:"server"`
	Version client.VersionInfo `json:"version"`
	Health  client.StatusInfo  `json:"health"`
	Ready   client.StatusInfo  `json:"ready"`
}

func writeServerStatusJSON(stdout io.Writer, server string, version client.VersionInfo, health, ready client.StatusInfo) error {
	snapshot := serverStatusSnapshot{
		Server:  server,
		Version: version,
		Health:  health,
		Ready:   ready,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func runServerWait(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server wait", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", defaultServerURL, "server base URL")
	timeout := fs.Duration("timeout", 30*time.Second, "maximum time to wait for readiness")
	interval := fs.Duration("interval", time.Second, "readiness check interval")
	jsonOutput := fs.Bool("json", false, "print server wait result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" {
		return errors.New("server URL is required")
	}
	if *timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if *interval <= 0 {
		return errors.New("interval must be positive")
	}

	api := client.New(*serverURL)
	deadline := time.Now().Add(*timeout)
	var lastErr error
	attempts := 0
	for {
		attempts++
		ready, err := api.Ready(ctx)
		if err == nil && strings.EqualFold(strings.TrimSpace(ready.Status), "ready") {
			if *jsonOutput {
				return writeServerWaitJSON(stdout, api.BaseURL, ready, attempts)
			}
			fmt.Fprintf(stdout, "server ready: %s\n", api.BaseURL)
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected readiness status: %s", ready.Status)
		}
		if !time.Now().Add(*interval).Before(deadline) {
			break
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("server was not ready before timeout %s: %w", timeout.String(), lastErr)
}

type serverWaitSnapshot struct {
	Server   string            `json:"server"`
	Ready    client.StatusInfo `json:"ready"`
	Attempts int               `json:"attempts"`
}

func writeServerWaitJSON(stdout io.Writer, server string, ready client.StatusInfo, attempts int) error {
	snapshot := serverWaitSnapshot{
		Server:   server,
		Ready:    ready,
		Attempts: attempts,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func runServerMetrics(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", defaultServerURL, "server base URL")
	jsonOutput := fs.Bool("json", false, "print server metrics as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" {
		return errors.New("server URL is required")
	}

	api := client.New(*serverURL)
	metrics, err := api.Metrics(ctx)
	if err != nil {
		return fmt.Errorf("metrics check failed: %w", err)
	}
	if *jsonOutput {
		return writeServerMetricsJSON(stdout, api.BaseURL, metrics)
	}
	fmt.Fprint(stdout, metrics)
	return nil
}

type serverMetricsSnapshot struct {
	Server  string `json:"server"`
	Bytes   int    `json:"bytes"`
	Metrics string `json:"metrics"`
}

func writeServerMetricsJSON(stdout io.Writer, server, metrics string) error {
	snapshot := serverMetricsSnapshot{
		Server:  server,
		Bytes:   len([]byte(metrics)),
		Metrics: metrics,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func runServerOpenAPI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server openapi", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", defaultServerURL, "server base URL")
	outputPath := fs.String("output", "", "write OpenAPI YAML to a file instead of stdout")
	jsonOutput := fs.Bool("json", false, "print OpenAPI result as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" {
		return errors.New("server URL is required")
	}

	api := client.New(*serverURL)
	spec, err := api.OpenAPI(ctx)
	if err != nil {
		return fmt.Errorf("openapi check failed: %w", err)
	}
	if strings.TrimSpace(*outputPath) != "" {
		if err := writeTextAtomically(*outputPath, spec); err != nil {
			return fmt.Errorf("write openapi output failed: %w", err)
		}
		if *jsonOutput {
			return writeServerOpenAPIJSON(stdout, serverOpenAPISnapshot{
				Server: api.BaseURL,
				Output: *outputPath,
				Bytes:  len([]byte(spec)),
			})
		}
		fmt.Fprintf(stdout, "openapi written: %s\n", *outputPath)
		return nil
	}
	if *jsonOutput {
		return writeServerOpenAPIJSON(stdout, serverOpenAPISnapshot{
			Server: api.BaseURL,
			Bytes:  len([]byte(spec)),
			Spec:   spec,
		})
	}
	fmt.Fprint(stdout, spec)
	return nil
}

type serverOpenAPISnapshot struct {
	Server string `json:"server"`
	Output string `json:"output,omitempty"`
	Bytes  int    `json:"bytes"`
	Spec   string `json:"spec,omitempty"`
}

func writeServerOpenAPIJSON(stdout io.Writer, snapshot serverOpenAPISnapshot) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot)
}

func writeTextAtomically(outputPath, content string) error {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return errors.New("output path is required")
	}
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if info, err := os.Stat(outputPath); err == nil && info.IsDir() {
		return fmt.Errorf("output path is a directory: %s", outputPath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".synchub-openapi-*")
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
	if _, err := io.WriteString(tmp, content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpName, outputPath); err != nil {
		return err
	}
	removeTmp = false
	return nil
}
