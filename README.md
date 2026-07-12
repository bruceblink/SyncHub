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
- Native desktop client
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

SyncHub Desktop -> REST API -> SyncHub Server -> Storage

## Tech Stack

- Go
- Gin
- PostgreSQL for server metadata
- Local FS / S3-compatible storage

## Roadmap

See docs/roadmap/ROADMAP.md

## User Guide

See [docs/user-guide.md](docs/user-guide.md) for local usage and manual testing steps.

## MVP Quick Start

Build the React admin bundle before running Go commands from a clean checkout. The generated `internal/api/admin_dist` directory is ignored by Git:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-web-admin.ps1
```

Run the API server with PostgreSQL. Production is the default environment, so `DATABASE_URL` is required:

The API loads missing values from `.env` in the current working directory. Existing process environment variables and deployment secrets take precedence.

```bash
$env:DATABASE_URL = "postgresql://user:password@host:5432/synchub?sslmode=require"
go run ./cmd/synchub-api
```

PostgreSQL migrations are applied automatically at startup. `DATABASE_URL` is required in every environment, including local development and tests.

The server listens on `http://localhost:8765` by default.

Useful endpoints:

- `GET /version`
- `GET /healthz`
- `GET /readyz` (includes database and storage readiness checks)
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

Build API server release artifacts:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.1.1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.1.1
```

The release directory also includes `docker-compose.release.yml` and `fly.toml` for deployment. It does not include a CLI binary.

See [docs/release-checklist.md](docs/release-checklist.md) for the release gate.
See [docs/releases/v0.1.1.md](docs/releases/v0.1.1.md) for the MVP release notes.

## Desktop Client

The end-user client is the companion `synchub-desktop` application. Configure the server URL, sign in, and initialize workspace folders in the desktop UI. It owns manifest scanning, push/pull, conflict preservation, trash recovery, diagnostics, and automatic background sync without invoking a CLI executable.

Existing login and workspace registry files are supported for migration from older releases. Create a `.synchubignore` file at a workspace root to exclude local build outputs or other paths from synchronization.

For the release Docker image on a Linux server:

```bash
docker pull ghcr.io/bruceblink/synchub:0.1.1
docker run -d --name synchub-api \
  -p 8765:8765 \
  -e JWT_SECRET=change-me \
  -e DATABASE_DRIVER=postgres \
  -e DATABASE_URL="$DATABASE_URL" \
  -v synchub-data:/data \
  ghcr.io/bruceblink/synchub:0.1.1
```

Or use the release compose file:

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.1.1
docker compose -f docker-compose.release.yml up -d
```

Or deploy to Fly.io from the project Dockerfile:

```powershell
# Edit fly.toml: set app name and primary_region.
fly apps create synchub-your-name
fly volumes create synchub_data --app synchub-your-name --region nrt --size 1
fly secrets set --app synchub-your-name JWT_SECRET="replace-with-a-long-random-secret"
fly secrets set --app synchub-your-name DATABASE_URL="postgresql://user:password@host:5432/synchub?sslmode=require"
fly deploy --config .\fly.toml
```

To use a Cloudflare-hosted custom domain, add a Fly certificate, copy the DNS
records from `fly certs setup`, and start with Cloudflare proxy disabled:

```powershell
$env:FLY_APP = "synchub-your-name"
$env:SYNCHUB_DOMAIN = "sync.example.com"

fly certs add $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly certs setup $env:SYNCHUB_DOMAIN --app $env:FLY_APP
fly certs check $env:SYNCHUB_DOMAIN --app $env:FLY_APP
curl.exe -fsS "https://$env:SYNCHUB_DOMAIN/readyz"
```

For automatic deployment, enable the Fly.io GitHub integration for this repository. The repository CI stays focused on tests, while Fly.io reports its own deployment check on push.

For a containerized local development server:

```bash
cp .env.example .env
docker compose up --build
```
