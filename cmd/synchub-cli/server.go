package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

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
