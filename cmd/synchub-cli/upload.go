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

func runUpload(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUploadUsage(stderr)
		return errors.New("upload command is required")
	}
	switch args[0] {
	case "status":
		return runUploadStatus(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUploadUsage(stdout)
		return nil
	default:
		printUploadUsage(stderr)
		return fmt.Errorf("unknown upload command: %s", args[0])
	}
}

func runUploadStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("upload status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootPath := fs.String("path", ".", "local workspace root")
	configPath := fs.String("config", defaultConfigPath(), "login config file path")
	workspaceConfigPath := fs.String("workspace-config", "", "workspace config file path")
	uploadID := fs.String("id", "", "upload session id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*uploadID) == "" {
		return errors.New("upload id is required")
	}

	session, err := openUploadCommandSession(ctx, *rootPath, *workspaceConfigPath, *configPath)
	if err != nil {
		return err
	}
	upload, err := session.apiClient.UploadStatus(ctx, session.accessToken, *uploadID)
	if err != nil {
		return err
	}
	printUploadStatus(stdout, upload)
	return nil
}

type uploadCommandSession struct {
	apiClient   *client.Client
	accessToken string
}

func openUploadCommandSession(ctx context.Context, rootPath, workspaceConfigPath, configPath string) (uploadCommandSession, error) {
	_, workspace, _, err := loadWorkspace(rootPath, workspaceConfigPath)
	if err != nil {
		return uploadCommandSession{}, err
	}
	loginConfig, err := readConfigWithRefresh(ctx, configPath)
	if err != nil {
		return uploadCommandSession{}, err
	}
	serverURL := workspace.ServerURL
	if strings.TrimSpace(serverURL) == "" {
		serverURL = loginConfig.ServerURL
	}
	return uploadCommandSession{
		apiClient:   client.New(serverURL),
		accessToken: loginConfig.Tokens.AccessToken,
	}, nil
}

func printUploadStatus(stdout io.Writer, upload client.UploadSession) {
	fmt.Fprintf(stdout, "upload: %s\n", upload.UploadID)
	fmt.Fprintf(stdout, "path: %s\n", upload.Path)
	fmt.Fprintf(stdout, "status: %s\n", upload.Status)
	fmt.Fprintf(stdout, "chunk size: %d\n", upload.ChunkSize)
	fmt.Fprintf(stdout, "expires at: %s\n", upload.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(stdout, "uploaded chunks: %d\n", len(upload.UploadedChunks))
	for _, chunk := range upload.UploadedChunks {
		fmt.Fprintf(stdout, "chunk %d size=%d sha256=%s\n", chunk.ChunkIndex, chunk.Size, chunk.SHA256)
	}
}
