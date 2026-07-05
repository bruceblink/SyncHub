# 运维手册

## 常见问题

- 上传失败
- 上传会话过期
- 同步延迟

## 排查

- logs: API 请求日志包含 method、path、status、duration_ms 和 trace_id，日志级别由 `LOG_LEVEL` 控制。
- metrics: `GET /metrics` 输出 Prometheus text format 的请求计数、状态族计数和耗时累计值，可用 4xx / 5xx 状态族估算错误率。
- readiness: `GET /readyz` 同时检查数据库和 storage。
- 过期上传会话由 API 进程内 worker 周期性标记为 `expired`，间隔由 `UPLOAD_CLEANUP_INTERVAL_SECONDS` 控制。
- 过期文件版本由 API 进程内 worker 周期性清理，间隔由 `VERSION_CLEANUP_INTERVAL_SECONDS` 控制；未设置时跟随上传清理间隔。
- 清理任务每轮处理数量由 `CLEANUP_BATCH_LIMIT` 控制，默认 `1000`。
- Agent 默认会持续重试同步；设置 `synchub-agent --cycles N` 可执行固定轮次后退出，适合本地验证和脚本化 smoke test。
- 设置 `synchub-agent --max-failures N` 后，连续失败达到 N 次会退出，便于由 Docker、systemd 或其他 supervisor 重启。
- 使用 `synchub-agent --path . --status` 可查看该工作区最近一次 Agent 运行状态、失败次数和最后错误；加 `--json` 可输出机器可读状态。
- 使用 `synchub-agent --path . --pause` / `--resume` 可通过工作区内控制文件暂停或恢复同步循环；加 `--json` 可输出机器可读控制结果。
- 使用 `synchub-agent --path . --reset-state` 可删除该工作区的 Agent 状态和暂停控制文件，适合本地重新验证同步循环；加 `--json` 可输出机器可读重置结果。

## Linux Docker 部署

发布版以 GHCR Docker 镜像为主交付物。Linux 服务器使用 Release 附带的 `docker-compose.release.yml`：

```bash
export JWT_SECRET=change-me
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.1.0
docker compose -f docker-compose.release.yml pull
docker compose -f docker-compose.release.yml up -d
docker compose -f docker-compose.release.yml ps
curl -fsS http://127.0.0.1:8765/readyz
```

升级时更新 `SYNCHUB_IMAGE` 后重复 `pull` 和 `up -d`。`synchub-data` volume 持久化 `/data/synchub.db` 和 `/data/storage`。

## 本地备份

开发环境默认使用 `.data/synchub.db` 和 `.data/storage`。在停止写入或停止 API 后执行：

```powershell
.\scripts\backup-local.ps1 -DataDir .data -OutputDir .backups
```

脚本会输出生成的 zip 路径。恢复时先停止 API，将 zip 中的 `synchub.db` 和 `storage` 放回同一个数据目录，再启动服务。

也可以使用恢复脚本。默认不会覆盖已有数据目录：

```powershell
.\scripts\restore-local.ps1 -BackupPath .backups\synchub-local-YYYYMMDD-HHMMSS.zip -DataDir .data
```

如果确认要替换现有 `.data`，添加 `-Force`。

备份 / 恢复脚本可以用临时数据目录做本地自检：

```powershell
.\scripts\test-local-backup-restore.ps1
```
