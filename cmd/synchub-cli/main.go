package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

const defaultServerURL = "http://localhost:8765"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("command is required")
	}
	switch args[0] {
	case "register":
		return runRegister(ctx, args[1:], stdout, stderr)
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr)
	case "workspace":
		return runWorkspace(args[1:], stdout, stderr)
	case "manifest":
		return runManifest(ctx, args[1:], stdout, stderr)
	case "file":
		return runFile(ctx, args[1:], stdout, stderr)
	case "sync":
		return runSync(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}
