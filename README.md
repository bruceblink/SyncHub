# SyncHub

Developer Workspace Sync Platform

## Vision

SyncHub is a Go-based developer workspace synchronization platform.

### Goals

- Workspace synchronization
- AI session synchronization
- File versioning
- WebDAV compatibility
- REST API
- CLI
- Multi-device synchronization

## Workspace

Supports:

- Claude Code
- Codex
- VS Code
- Cursor
- Obsidian
- Git
- SSH

## Architecture

Agent -> REST API -> SyncHub Server -> Storage

## Tech Stack

- Go
- Gin
- SQLite for local development
- PostgreSQL / MySQL adapters later
- Local FS / S3-compatible storage

## Roadmap

See docs/roadmap/ROADMAP.md

## MVP Quick Start

Run the API server with the default SQLite database and local file storage:

```bash
go run ./cmd/synchub-api
```

The server listens on `http://localhost:8765` by default.

Useful endpoints:

- `GET /healthz`
- `GET /readyz`
- `GET /swagger/`
- `GET /swagger/openapi.yaml`

Run the MVP checks:

```bash
go fmt ./...
go vet ./...
go test ./...
```

Minimal CLI flow:

```bash
go run ./cmd/synchub-cli register --server http://localhost:8765 --email user@example.com --password password
go run ./cmd/synchub-cli workspace init --path . --remote-path /workspace
go run ./cmd/synchub-cli sync once --path .
go run ./cmd/synchub-cli sync status --path .
```

Run the agent loop for an initialized workspace:

```bash
go run ./cmd/synchub-agent --path .
```

For a single agent sync cycle, add `--once`.

For a containerized local server:

```bash
docker compose up --build
```
