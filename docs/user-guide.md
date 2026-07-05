# SyncHub 使用与测试手册

本文面向本地 MVP 使用测试，默认在 Windows PowerShell 下执行，项目根目录为 `F:\project\SyncHub`。

当前 MVP 使用 SQLite 作为数据库、Local FS 作为对象存储，默认数据目录是 `.data`：

- 数据库：`.data/synchub.db`
- 文件对象：`.data/storage`
- API 地址：`http://localhost:8765`

## 1. 环境准备

确认 Go 和 PowerShell 可用：

```powershell
go version
pwsh -NoProfile -Command '$PSVersionTable.PSVersion'
```

如果没有 `pwsh`，也可以使用 Windows PowerShell：

```powershell
powershell -NoProfile -Command '$PSVersionTable.PSVersion'
```

进入项目根目录：

```powershell
cd F:\project\SyncHub
```

建议先跑一次基础验证：

```powershell
go test ./...
```

## 2. 一键 MVP 验收

最省心的验证方式是运行项目内置脚本：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
```

该脚本会串联执行：

- `gofmt` 检查
- `go vet ./...`
- `go test ./...`
- 本地 API smoke test
- 本地备份恢复 smoke test

看到以下输出表示自动验收通过：

```text
MVP checks passed
```

如果只想跑 API 闭环 smoke test：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-local-api-smoke.ps1
```

这个脚本会自动启动临时 API 服务，验证注册、工作区初始化、同步、Agent 暂停恢复、版本历史、pin/unpin、restore 和下载。

## 3. 手动启动 API

开发环境可以直接运行 API：

```powershell
$env:HTTP_ADDR = ":8765"
$env:DATABASE_DRIVER = "sqlite"
$env:DATABASE_URL = ".\.data\synchub.db"
$env:LOCAL_STORAGE_ROOT = ".\.data\storage"
$env:JWT_SECRET = "local-dev-secret"
go run .\cmd\synchub-api
```

保持这个窗口运行。另开一个 PowerShell 窗口执行 CLI 命令。

检查服务是否可用：

```powershell
go run .\cmd\synchub-cli server wait --server http://localhost:8765 --timeout 30s
go run .\cmd\synchub-cli server status --server http://localhost:8765
go run .\cmd\synchub-cli server metrics --server http://localhost:8765
```

浏览器可打开：

- `http://localhost:8765/healthz`
- `http://localhost:8765/readyz`
- `http://localhost:8765/swagger/`

## 4. 准备本地测试目录

为了模拟两台设备，创建两个本地目录：

```powershell
$root = "F:\tmp\synchub-manual"
$server = "http://localhost:8765"
$login = Join-Path $root "login.json"
$deviceA = Join-Path $root "device-a"
$deviceB = Join-Path $root "device-b"

Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $deviceA, $deviceB | Out-Null
```

注册用户。邮箱可以每次换一个，避免和旧数据冲突：

```powershell
$email = "manual-$([guid]::NewGuid().ToString('N'))@example.com"
$password = "password123"

go run .\cmd\synchub-cli register `
  --server $server `
  --email $email `
  --password $password `
  --config $login
```

初始化两个工作区，映射到同一个远端目录 `/workspace`：

```powershell
go run .\cmd\synchub-cli workspace init --path $deviceA --remote-path /workspace --config $login
go run .\cmd\synchub-cli workspace init --path $deviceB --remote-path /workspace --config $login
```

同步前可以先运行诊断命令，集中检查工作区配置、登录配置、服务端 ready、认证、设备注册、manifest 和 Agent 暂停状态：

```powershell
go run .\cmd\synchub-cli sync doctor --path $deviceA --config $login
go run .\cmd\synchub-cli sync doctor --path $deviceA --config $login --json
```

如果只出现 `warn`，通常表示还缺少第一次同步产生的状态，例如设备 ID 或 manifest；按输出里的 `next` 命令继续即可。如果出现 `fail`，先修复登录、服务端或工作区配置问题。

## 5. 验证上传和下载同步

在设备 A 创建文件：

```powershell
Set-Content -LiteralPath (Join-Path $deviceA "hello.txt") -Value "hello from device A" -NoNewline
```

先 dry run 看计划：

```powershell
go run .\cmd\synchub-cli sync once --path $deviceA --config $login --device-name device-a --platform windows --dry-run
```

执行同步：

```powershell
go run .\cmd\synchub-cli sync once --path $deviceA --config $login --device-name device-a --platform windows
```

在设备 B 拉取：

```powershell
go run .\cmd\synchub-cli sync once --path $deviceB --config $login --device-name device-b --platform windows
Get-Content -LiteralPath (Join-Path $deviceB "hello.txt")
```

如果输出为 `hello from device A`，说明新增文件同步成功。

## 6. 验证修改和版本历史

在设备 A 修改同一个文件：

```powershell
Set-Content -LiteralPath (Join-Path $deviceA "hello.txt") -Value "hello version two" -NoNewline
go run .\cmd\synchub-cli sync once --path $deviceA --config $login --device-name device-a --platform windows
```

查看版本历史：

```powershell
go run .\cmd\synchub-cli file versions --path $deviceA --config $login --remote-path /workspace/hello.txt --limit 10
```

期望看到至少两个版本，例如：

```text
versions: 2
v2 ...
v1 ...
```

pin 第一个版本：

```powershell
go run .\cmd\synchub-cli file pin --path $deviceA --config $login --remote-path /workspace/hello.txt --version 1
```

取消 pin：

```powershell
go run .\cmd\synchub-cli file unpin --path $deviceA --config $login --remote-path /workspace/hello.txt --version 1
```

恢复到第一个版本：

```powershell
go run .\cmd\synchub-cli file restore --path $deviceA --config $login --remote-path /workspace/hello.txt --version 1
```

下载恢复后的远端文件到临时路径检查内容：

```powershell
$restored = Join-Path $root "restored-hello.txt"
go run .\cmd\synchub-cli file download --path $deviceA --config $login --remote-path /workspace/hello.txt --output $restored
Get-Content -LiteralPath $restored
```

期望内容回到最初的 `hello from device A`。

## 7. 验证删除和本地 trash

在设备 A 删除文件并同步：

```powershell
Remove-Item -LiteralPath (Join-Path $deviceA "hello.txt")
go run .\cmd\synchub-cli sync once --path $deviceA --config $login --device-name device-a --platform windows
```

在设备 B 拉取删除事件：

```powershell
go run .\cmd\synchub-cli sync once --path $deviceB --config $login --device-name device-b --platform windows
go run .\cmd\synchub-cli sync trash --path $deviceB
go run .\cmd\synchub-cli sync trash --path $deviceB --json
```

被远端删除影响的本地文件会移动到 `.synchub/trash`，避免直接丢失。

也可以查看整体状态：

```powershell
go run .\cmd\synchub-cli sync status --path $deviceB
```

## 8. 验证 Agent

Agent 是对 `synchub-cli sync once` 的循环封装。

执行一次 Agent 同步：

```powershell
go run .\cmd\synchub-agent --path $deviceA --config $login --once --device-name device-a --platform windows
go run .\cmd\synchub-agent --path $deviceA --config $login --once --device-name device-a --platform windows --json
```

查看 Agent 状态：

```powershell
go run .\cmd\synchub-agent --path $deviceA --status
go run .\cmd\synchub-agent --path $deviceA --status --json
```

状态输出中的 `paused` 表示当前工作区是否存在暂停控制文件；即使 Agent 尚未运行过，也可以用它确认暂停开关是否生效。

暂停同步：

```powershell
go run .\cmd\synchub-agent --path $deviceA --pause
go run .\cmd\synchub-agent --path $deviceA --once --config $login
```

暂停后执行 `--once` 应输出：

```text
sync skipped: agent is paused
```

恢复同步：

```powershell
go run .\cmd\synchub-agent --path $deviceA --resume
```

清理 Agent 状态文件，便于重新验证：

```powershell
go run .\cmd\synchub-agent --path $deviceA --reset-state
```

持续监听本地变化：

```powershell
go run .\cmd\synchub-agent --path $deviceA --config $login --watch --watch-interval 1s --interval 30s --device-name device-a --platform windows
```

## 9. 常用命令速查

服务端：

```powershell
go run .\cmd\synchub-cli server status --server http://localhost:8765
go run .\cmd\synchub-cli server metrics --server http://localhost:8765
go run .\cmd\synchub-cli server openapi --server http://localhost:8765 --output .\openapi.yaml
```

认证：

```powershell
go run .\cmd\synchub-cli register --server http://localhost:8765 --email user@example.com --password password --config .\.data\login.json
go run .\cmd\synchub-cli login --server http://localhost:8765 --email user@example.com --password password --config .\.data\login.json
go run .\cmd\synchub-cli logout --config .\.data\login.json
```

文件操作：

`file mkdir`、`file move`、`file delete` 和 `file restore` 会在工作区缺少设备 ID 时自动注册当前设备，并把设备 ID 写回 `.synchub/workspace.json`。这样这些手动文件操作产生的远端变更也能被后续同步正确识别为本机变更。

```powershell
go run .\cmd\synchub-cli file list --path $deviceA --config $login
go run .\cmd\synchub-cli file mkdir --path $deviceA --config $login --remote-path /workspace/docs
go run .\cmd\synchub-cli file move --path $deviceA --config $login --remote-path /workspace/a.txt --to /workspace/docs/a.txt
go run .\cmd\synchub-cli file delete --path $deviceA --config $login --remote-path /workspace/docs/a.txt
go run .\cmd\synchub-cli file download --path $deviceA --config $login --remote-path /workspace/docs/a.txt --output .\a.txt
```

同步：

```powershell
go run .\cmd\synchub-cli sync status --path $deviceA
go run .\cmd\synchub-cli sync status --path $deviceA --json
go run .\cmd\synchub-cli sync doctor --path $deviceA --config $login
go run .\cmd\synchub-cli sync status --path $deviceA --config $login --show-remote --show-conflicts
go run .\cmd\synchub-cli sync devices --path $deviceA --config $login
go run .\cmd\synchub-cli sync conflicts --path $deviceA --config $login
go run .\cmd\synchub-cli sync conflicts resolve --path $deviceA --config $login --id conf_1 --resolution keep_both --json
go run .\cmd\synchub-cli sync trash --path $deviceA
go run .\cmd\synchub-cli sync trash --path $deviceA --json
go run .\cmd\synchub-cli sync trash restore --path $deviceA --batch 20260702T010000.000000000Z --entry docs/readme.txt --json
```

## 10. Docker Compose

推荐使用 Docker Compose 启动本地服务：

```powershell
$env:SYNCHUB_VERSION = "0.0.1"
$env:GOPROXY = "https://goproxy.cn,direct"
docker compose up --build
```

服务启动后仍然使用：

```powershell
go run .\cmd\synchub-cli server status --server http://localhost:8765
```

也可以用脚本一次性验证 compose 构建、启动和 readyz：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-compose.ps1
```

如果只想构建镜像，不启动 compose：

```powershell
docker build --network=host --pull=false `
  --build-arg VERSION=0.0.1 `
  --build-arg GOPROXY=https://goproxy.cn,direct `
  -t synchub:0.0.1 .
```

如果 Docker build 卡在 `golang:*` 或 `alpine:*` 镜像元数据拉取，先检查 Docker registry mirror。当前本地验证可用配置为：

```powershell
docker info --format '{{json .RegistryConfig.Mirrors}}'
```

期望包含：

```json
["https://hub.rat.dev/"]
```

如果 Go module 下载在容器内超时或 EOF，优先保留 compose 中的 `build.network: host`，或在单独构建时使用 `--network=host`。当前 MVP 也可以不依赖 Docker，通过 `go run .\cmd\synchub-api` 完成本地功能验证。

## 11. 备份和恢复本地数据

停止 API 写入后执行备份：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\backup-local.ps1 -DataDir .data -OutputDir .backups
```

恢复到 `.data`：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\restore-local.ps1 -BackupPath .backups\synchub-local-YYYYMMDD-HHMMSS.zip -DataDir .data
```

如果目标数据目录已存在并确认覆盖：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\restore-local.ps1 -BackupPath .backups\synchub-local-YYYYMMDD-HHMMSS.zip -DataDir .data -Force
```

备份恢复自检：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-local-backup-restore.ps1
```

## 12. 排查建议

服务不可用：

```powershell
go run .\cmd\synchub-cli server wait --server http://localhost:8765 --timeout 30s
go run .\cmd\synchub-cli server status --server http://localhost:8765
```

工作区未初始化：

```powershell
go run .\cmd\synchub-cli workspace init --path $deviceA --remote-path /workspace --config $login
```

先预览同步计划：

```powershell
go run .\cmd\synchub-cli sync once --path $deviceA --config $login --dry-run
```

查看本地同步状态：

```powershell
go run .\cmd\synchub-cli sync status --path $deviceA --config $login --show-remote --show-conflicts
```

重新开始一轮干净的本地测试：

```powershell
Stop-Process -Name synchub-api -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath .\.data -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath F:\tmp\synchub-manual -Recurse -Force -ErrorAction SilentlyContinue
```
