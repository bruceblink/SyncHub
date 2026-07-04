package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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

	fmt.Fprintf(stdout, "server: %s\n", api.BaseURL)
	fmt.Fprintf(stdout, "version: %s %s\n", version.Name, version.Version)
	fmt.Fprintf(stdout, "health: %s\n", health.Status)
	fmt.Fprintf(stdout, "ready: %s\n", ready.Status)
	return nil
}

func runServerWait(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server wait", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", defaultServerURL, "server base URL")
	timeout := fs.Duration("timeout", 30*time.Second, "maximum time to wait for readiness")
	interval := fs.Duration("interval", time.Second, "readiness check interval")
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
	for {
		ready, err := api.Ready(ctx)
		if err == nil && strings.EqualFold(strings.TrimSpace(ready.Status), "ready") {
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

func runServerMetrics(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("server metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server", defaultServerURL, "server base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*serverURL) == "" {
		return errors.New("server URL is required")
	}

	metrics, err := client.New(*serverURL).Metrics(ctx)
	if err != nil {
		return fmt.Errorf("metrics check failed: %w", err)
	}
	fmt.Fprint(stdout, metrics)
	return nil
}
