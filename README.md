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

Check local binary versions:

```bash
go run ./cmd/synchub-cli version
go run ./cmd/synchub-agent --version
go run ./cmd/synchub-cli server wait --server http://localhost:8765 --timeout 30s
go run ./cmd/synchub-cli server status --server http://localhost:8765
go run ./cmd/synchub-cli server metrics --server http://localhost:8765
go run ./cmd/synchub-cli server openapi --server http://localhost:8765
```

Useful endpoints:

- `GET /version`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /swagger/`
- `GET /swagger/openapi.yaml`

Run the MVP checks:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
```

The MVP check script runs formatting, vet, unit/integration tests, local API smoke checks, and local backup/restore smoke checks.

Minimal CLI flow:

```bash
go run ./cmd/synchub-cli register --server http://localhost:8765 --email user@example.com --password password
go run ./cmd/synchub-cli workspace init --path . --remote-path /workspace
go run ./cmd/synchub-cli sync once --path . --dry-run
go run ./cmd/synchub-cli sync once --path .
go run ./cmd/synchub-cli sync status --path .
go run ./cmd/synchub-cli sync trash --path .
go run ./cmd/synchub-cli sync devices --path .
```

Use `sync once --dry-run` before applying changes if you want to inspect the local push plan and incoming change feed.
Use `sync trash` to inspect local files moved aside after remote delete events.
`sync status` also shows a local trash summary when these files exist.
Create a `.synchubignore` file at the workspace root to exclude local build outputs or other paths from manifest scanning, watch detection, and sync push.

Run the agent loop for an initialized workspace:

```bash
go run ./cmd/synchub-agent --path .
```

For a single agent sync cycle, add `--once`.
For an agent-driven preview, use `--once --dry-run`.
Use `--device-name`, `--platform`, and `--limit` to control device registration and pull batch size.
Use `--cycles N` to run a fixed number of agent sync cycles and then exit.
Use `--max-failures N` to make the agent exit after N consecutive sync failures, so an external supervisor can restart it.
Use `--watch` to trigger an extra sync cycle when local workspace changes are detected between scheduled intervals.
Use `--status` to print the last recorded agent state for the workspace.
Use `--pause` and `--resume` to stop or restart sync cycles for a workspace without changing its configuration.

For a containerized local server:

```bash
docker compose up --build
```
