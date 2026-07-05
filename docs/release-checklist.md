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

## 3. Optional Docker Smoke

Run this when Docker Desktop and registry access are healthy:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-compose.ps1 -Version 0.1.0
```

## 4. Final Git Gate

```powershell
git status --short
git log --oneline -5
```

The worktree should be clean before tagging. Use `git tag v0.1.0` only after the MVP check and release build both pass.
