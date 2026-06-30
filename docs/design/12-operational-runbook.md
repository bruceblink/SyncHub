# 运维手册

## 常见问题

- 上传失败
- 上传会话过期
- 同步延迟

## 排查

- logs
- metrics
- 过期上传会话由 API 进程内 worker 周期性标记为 `expired`，间隔由 `UPLOAD_CLEANUP_INTERVAL_SECONDS` 控制。
