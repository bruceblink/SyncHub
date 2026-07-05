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

synchub-cli -> REST API -> SyncHub Server -> Storage

## Tech Stack

- Go
- Gin
- SQLite for local development
- PostgreSQL / MySQL adapters later
- Local FS / S3-compatible storage

## Roadmap

See docs/roadmap/ROADMAP.md

## User Guide

See [docs/user-guide.md](docs/user-guide.md) for local usage and manual testing steps.

## MVP Quick Start

Run the API server with the default SQLite database and local file storage:

```bash
go run ./cmd/synchub-api
```

The server listens on `http://localhost:8765` by default.

Check local binary versions:

```bash
go run ./cmd/synchub-cli version
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

Build and smoke-test the MVP Docker image:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.1.1 -Image synchub:0.1.1
```

Build auxiliary MVP release artifacts:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.1.1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.1.1
```

The auxiliary release directory also includes `docker-compose.release.yml` and `fly.toml` for deployment.

See [docs/release-checklist.md](docs/release-checklist.md) for the release gate.
See [docs/releases/v0.1.1.md](docs/releases/v0.1.1.md) for the MVP release notes.

Minimal CLI flow:

```bash
go run ./cmd/synchub-cli register --server http://localhost:8765 --email user@example.com --password password
go run ./cmd/synchub-cli workspace init --path . --remote-path /workspace
go run ./cmd/synchub-cli sync doctor --path .
go run ./cmd/synchub-cli sync once --path . --dry-run
go run ./cmd/synchub-cli sync once --path .
go run ./cmd/synchub-cli sync status --path .
go run ./cmd/synchub-cli sync trash --path .
go run ./cmd/synchub-cli sync devices --path .
```

Initialize several workspaces in one command by passing multiple paths and a shared remote parent:

```bash
go run ./cmd/synchub-cli workspace init --remote-root /workspace ./notes ./code
```

Use `sync doctor` to check workspace config, login config, API readiness, auth, device registration, manifest state, and daemon pause state before manual testing.
Use `sync once --dry-run` before applying changes if you want to inspect the local push plan and incoming change feed.
Use `sync trash` to inspect local files moved aside after remote delete events.
`sync status` also shows a local trash summary when these files exist.
Create a `.synchubignore` file at the workspace root to exclude local build outputs or other paths from manifest scanning, watch detection, and sync push. The `.synchubignore` file is synchronized like a normal workspace file so devices share the same rules.

`workspace init` registers the workspace in the user-level workspace registry. A login/startup task can then run one command to watch every registered workspace:

```bash
go run ./cmd/synchub-cli sync daemon
```

The daemon does not depend on the current directory when `--path` is omitted. It loads the registered workspaces for the current login config, watches local changes by default, and also runs a scheduled sync as a fallback. Use `--path <workspace>` only when you want to operate on one workspace explicitly.
For a single daemon sync cycle, add `--once`.
For a daemon-driven preview, use `--once --dry-run`.
Use `--once --json` or `--once --dry-run --json` when an external client needs the underlying `sync once` result in a machine-readable format.
Use `--device-name`, `--platform`, and `--limit` to control device registration and pull batch size.
Use `--cycles N` to run a fixed number of daemon sync cycles and then exit.
Use `--max-failures N` to make the daemon exit after N consecutive sync failures, so an external supervisor can restart it.
Use `--no-watch` if you only want interval-based sync.
Use `--status` to print the last recorded daemon state for the workspace.
Use `--status --json` to print the last recorded daemon state in a machine-readable format.
Use `--pause` and `--resume` to stop or restart sync cycles for a workspace without changing its configuration.
Use `--pause --json` or `--resume --json` when an external client needs a machine-readable control result.
Use `--reset-state` to delete the workspace daemon state and pause control files before rerunning local verification.
Use `--reset-state --json` when an external client needs a machine-readable reset result.

For the release Docker image on a Linux server:

```bash
docker pull ghcr.io/bruceblink/synchub:0.1.1
docker run -d --name synchub-api \
  -p 8765:8765 \
  -e JWT_SECRET=change-me \
  -v synchub-data:/data \
  ghcr.io/bruceblink/synchub:0.1.1
```

Or use the release compose file:

```bash
export JWT_SECRET=change-me
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.1.1
docker compose -f docker-compose.release.yml up -d
```

Or deploy to Fly.io from the project Dockerfile:

```powershell
# Edit fly.toml: set app name and primary_region.
fly apps create synchub-your-name
fly volumes create synchub_data --app synchub-your-name --region nrt --size 1
fly secrets set --app synchub-your-name JWT_SECRET="replace-with-a-long-random-secret"
fly deploy --config .\fly.toml
```

GitHub Actions deploys the Fly app automatically after the `main` branch CI job passes. Configure the repository secret `FLY_API_TOKEN` before relying on CI deployment.

For a containerized local development server:

```bash
docker compose up --build
```
