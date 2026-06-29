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
- PostgreSQL
- pgx + sqlc
- Local FS / S3-compatible storage

## Roadmap
See docs/roadmap/ROADMAP.md
