# SyncHub MVP Release Checklist

This checklist is the minimum gate before publishing an MVP release.

## 1. Verify The MVP

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
```

Expected final line:

```text
MVP checks passed
```

## 2. Build And Smoke Test The Docker Image

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.1.1 -Image synchub:0.1.1
```

The release Docker image is the primary deployment artifact. The script builds the image, checks the embedded CLI version, starts the API container, and verifies `/readyz` plus `/version`.

## 3. Build Release Assets

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.1.1
```

The script writes the release deployment files, auxiliary Linux CLI/server archives, a Windows developer archive, and `SHA256SUMS.txt` under `dist\synchub-0.1.1`. The archives are fallback/manual-install artifacts; Docker images plus `docker-compose.release.yml` or `fly.toml` are the normal release and deployment path.

## 4. Verify Release Assets

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.1.1
```

The verifier checks the deployment compose file, expected archives, SHA256 hashes, required bundled files, the host-platform CLI version output, and the integrated CLI daemon help.

## 5. Review Release Notes

Review [docs/releases/v0.1.1.md](releases/v0.1.1.md) before tagging.

## 6. Optional Docker Compose Smoke

Run this when Docker Desktop and registry access are healthy:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-compose.ps1 -Version 0.1.1
```

## 7. Final Git Gate

```powershell
git status --short
git log --oneline -5
```

The worktree should be clean before tagging. Use `git tag --no-sign v0.1.1` only after the MVP check, Docker image smoke, and auxiliary artifact checks pass.

## 8. Publish

```powershell
git push origin main
git push origin v0.1.1
```

Pushing a `v*` tag triggers the Release workflow on Linux. The workflow reruns the deterministic MVP gate, smoke-tests the Docker image, pushes `ghcr.io/bruceblink/synchub:<version>` plus matching tags, and publishes the GitHub Release with `docker-compose.release.yml`, `fly.toml`, auxiliary archives, and `SHA256SUMS.txt`. The full local MVP gate above remains the pre-tag check for the local API smoke flow.
