# SyncHub

[简体中文](README.md) | [English](README.en.md)

SyncHub is a multi-device synchronization platform for developer workspaces. This repository contains the Go API server and React admin interface. End users synchronize through the companion SyncHub Desktop application; no CLI installation is required.

## Architecture

```text
SyncHub Desktop -> REST API -> SyncHub Server -> PostgreSQL + Object Storage
                              -> React Admin
```

Core capabilities:

- Authentication, device registration, and sync cursors
- File upload, download, directory management, and soft deletion
- File versions, version pinning, and historical restore
- Change events, conflict records, and trash recovery
- PostgreSQL metadata and Local FS / S3-compatible storage abstraction
- Health checks, metrics, Swagger, and a React admin interface

## Quick Start

Prepare `.env`. `DATABASE_URL` is required in every environment:

```dotenv
DATABASE_URL=postgresql://user:password@host:5432/synchub?sslmode=require
JWT_SECRET=replace-with-a-long-random-secret
```

Build the admin interface and start the API:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-web-admin.ps1
go run .\cmd\synchub-api
```

The server listens on `http://localhost:8765` by default and applies missing PostgreSQL migrations during startup. Process environment variables and deployment secrets take precedence over `.env`.

Useful endpoints:

- `GET /version`
- `GET /healthz`
- `GET /readyz`, including database and storage checks
- `GET /metrics`
- `GET /swagger/`
- `GET /swagger/openapi.yaml`

## Desktop Client

The companion client lives at `F:\project\synchub-desktop`. Configure the server URL, sign in, and initialize workspace folders in the desktop application. It then runs background synchronization and provides file, version, conflict, device, and trash management.

Existing login and workspace registry files remain readable for lossless upgrades, but server releases no longer include a CLI binary.

## Verification

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.1.1 -Image synchub:0.1.1
```

`test-mvp.ps1` builds the React admin interface and runs Go formatting, vet, and the complete test suite. The Docker smoke test validates image metadata, runtime contents, `/readyz`, and `/version`.

## Release And Deployment

Build and verify API-only release archives:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.1.1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.1.1
```

Docker Compose:

```bash
export JWT_SECRET=change-me
export DATABASE_URL='postgresql://user:password@host:5432/synchub?sslmode=require'
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.1.1
docker compose -f docker-compose.release.yml up -d
```

Fly.io:

```powershell
fly apps create synchub-your-name
fly volumes create synchub_data --app synchub-your-name --region nrt --size 20
fly secrets set --app synchub-your-name JWT_SECRET="replace-with-a-long-random-secret"
fly secrets set --app synchub-your-name DATABASE_URL="postgresql://user:password@host:5432/synchub?sslmode=require"
fly deploy --config .\fly.toml
```

Further documentation:

- [User guide](docs/user-guide.md)
- [Deployment design](docs/design/08-deployment.md)
- [Release checklist](docs/release-checklist.md)
- [Roadmap](docs/roadmap/ROADMAP.md)
