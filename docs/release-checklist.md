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

## 2. Build Release Artifacts

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-release.ps1 -Version 0.1.0
```

The script writes versioned archives and `SHA256SUMS.txt` under `dist\synchub-0.1.0`.

## 3. Verify Release Artifacts

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\verify-release.ps1 -Version 0.1.0
```

The verifier checks the expected archives, SHA256 hashes, required bundled files, and the host-platform CLI/agent version output.

## 4. Review Release Notes

Review [docs/releases/v0.1.0.md](releases/v0.1.0.md) before tagging.

## 5. Optional Docker Smoke

Run this when Docker Desktop and registry access are healthy:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-compose.ps1 -Version 0.1.0
```

## 6. Final Git Gate

```powershell
git status --short
git log --oneline -5
```

The worktree should be clean before tagging. Use `git tag --no-sign v0.1.0` only after the MVP check and release build both pass.

## 7. Publish

```powershell
git push origin main
git push origin v0.1.0
```

Pushing a `v*` tag triggers the Release workflow. The workflow reruns the deterministic MVP gate, rebuilds and verifies release artifacts, then publishes the GitHub Release with the archives and `SHA256SUMS.txt`. The full local MVP gate above remains the pre-tag check for the local API smoke flow.
