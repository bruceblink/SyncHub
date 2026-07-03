# 运维手册

## 常见问题

- 上传失败
- 上传会话过期
- 同步延迟

## 排查

- logs
- metrics
- 过期上传会话由 API 进程内 worker 周期性标记为 `expired`，间隔由 `UPLOAD_CLEANUP_INTERVAL_SECONDS` 控制。

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
