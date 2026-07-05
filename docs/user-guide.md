# SyncHub 用户手册

本文面向 SyncHub MVP 用户，说明如何部署服务端、安装 CLI、初始化工作区，并完成日常同步、版本恢复、备份和排查。

当前 MVP 的推荐形态是：

- 服务端：Linux 服务器 + Docker 镜像 + SQLite + 本地文件存储。
- 客户端：只需要安装 `synchub-cli`。
- 后台同步：`synchub-cli sync daemon` 默认监听本地变更，并按周期兜底同步。

当前 MVP 暂不包含 WebDAV、S3/OSS/MinIO、团队空间、共享目录和生产级 PostgreSQL/MySQL adapter。

下面示例以 `0.1.1` 为版本号；实际使用时请替换为当前最新 Release 版本。

## 1. 核心概念

| 名称 | 说明 |
| --- | --- |
| API Server | SyncHub 服务端，负责认证、元数据、版本历史、变更日志和文件对象存储。 |
| Workspace | 本地要同步的目录，例如一个项目目录、笔记目录或 AI 会话目录。 |
| Remote Path | 服务端上的远端根路径，例如 `/workspace`、`/notes`。多台设备使用同一个远端路径即可同步同一批文件。 |
| Login Config | CLI 登录配置，保存服务端地址、用户信息和 token。不要提交到 Git。 |
| Workspace Config | 工作区配置，默认写入 `<workspace>/.synchub/workspace.json`。 |
| Manifest | 本地文件快照，默认写入 `<workspace>/.synchub/manifest.json`，用于比较增量变化。 |
| Trash | 远端删除同步到本地时，本地文件会先移动到 `<workspace>/.synchub/trash`，避免直接丢失。 |

## 2. Linux 服务器部署

发布版以 Docker 镜像作为主要交付物。服务器只需要 Docker 和 Docker Compose。

### 2.1 使用 Compose 部署

在 Linux 服务器上创建部署目录：

```bash
mkdir -p ~/synchub
cd ~/synchub
```

下载发布版 Compose 文件：

```bash
curl -L -o docker-compose.release.yml \
  https://github.com/bruceblink/SyncHub/releases/download/v0.1.1/docker-compose.release.yml
```

设置镜像和密钥：

```bash
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:0.1.1
export JWT_SECRET='replace-with-a-long-random-secret'
```

启动服务：

```bash
docker compose -f docker-compose.release.yml up -d
```

检查服务状态：

```bash
docker compose -f docker-compose.release.yml ps
curl -fsS http://127.0.0.1:8765/readyz
curl -fsS http://127.0.0.1:8765/version
```

如果 GHCR 镜像不是公开可拉取，需要先登录：

```bash
docker login ghcr.io
```

### 2.2 使用 docker run 快速启动

只想快速试用时，也可以直接运行容器：

```bash
docker run -d --name synchub-api \
  -p 8765:8765 \
  -e JWT_SECRET='replace-with-a-long-random-secret' \
  -v synchub-data:/data \
  ghcr.io/bruceblink/synchub:0.1.1
```

容器内默认数据路径：

- SQLite 数据库：`/data/synchub.db`
- 文件对象：`/data/storage`

### 2.3 部署到 Fly.io

SyncHub MVP 可以直接部署到 Fly.io。当前推荐使用单个 Fly Machine + 一个挂载到 `/data` 的 Fly Volume：

- `/data/synchub.db` 保存 SQLite 数据库。
- `/data/storage` 保存文件对象。
- `JWT_SECRET` 使用 Fly secrets 设置，不写入 `fly.toml`。
- 不要把 Machine 数量扩到 2 个或更多；Fly Volume 不会自动复制，当前 SQLite + 本地存储适合单实例部署。

在 Windows 开发机安装并登录 flyctl：

```powershell
pwsh -Command "iwr https://fly.io/install.ps1 -useb | iex"
fly auth login
```

编辑项目根目录或 Release 附带的 `fly.toml`：

- 把 `app = "synchub-your-name"` 改成全局唯一的 Fly app 名称。
- 按需要调整 `primary_region`，例如 `nrt`、`hkg`、`sin`、`sjc`。
- 默认使用项目根目录的 `Dockerfile` 在 Fly remote builder 上构建，不再需要在 `fly.toml` 固定镜像版本。

创建 App 和数据卷：

```powershell
$env:FLY_APP = "synchub-your-name"
$env:FLY_REGION = "nrt"

fly apps create $env:FLY_APP
fly volumes create synchub_data --app $env:FLY_APP --region $env:FLY_REGION --size 1
```

设置服务端密钥：

```powershell
fly secrets set --app $env:FLY_APP JWT_SECRET="replace-with-a-long-random-secret"
```

部署：

```powershell
fly deploy --config .\fly.toml
```

如果使用 GitHub Actions 自动部署，在仓库 Secrets 中设置 `FLY_API_TOKEN`。之后推送到 `main` 时，CI 测试通过后会自动执行 Fly 部署。

检查服务：

```powershell
fly status --app $env:FLY_APP
fly checks list --app $env:FLY_APP
fly logs --app $env:FLY_APP
curl.exe -fsS "https://$env:FLY_APP.fly.dev/readyz"
curl.exe -fsS "https://$env:FLY_APP.fly.dev/version"
```

后续 CLI 登录时，服务端地址就是 Fly 提供的 HTTPS 地址：

```powershell
$env:SYNCHUB_SERVER = "https://$env:FLY_APP.fly.dev"
synchub-cli register --server $env:SYNCHUB_SERVER --email user@example.com --password "change-me"
synchub-cli login --server $env:SYNCHUB_SERVER --email user@example.com --password "change-me"
```

手动升级时，从最新代码重新部署：

```powershell
fly deploy --config .\fly.toml
curl.exe -fsS "https://$env:FLY_APP.fly.dev/readyz"
```

Fly 会为 Volume 提供自动快照，但不要把它当作唯一备份。重要数据建议定期导出 `/data` 或至少保留可恢复的 Volume snapshot。

### 2.4 常用服务端环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `JWT_SECRET` | 无 | 必填。用于签发 token，请使用长随机字符串。 |
| `HTTP_ADDR` | `:8765` | API 监听地址。 |
| `DATABASE_DRIVER` | `sqlite` | MVP 推荐保持默认。 |
| `DATABASE_URL` | `/data/synchub.db` | 容器内 SQLite 数据库路径。 |
| `STORAGE_BACKEND` | `local` | MVP 推荐保持默认。 |
| `LOCAL_STORAGE_ROOT` | `/data/storage` | 容器内文件对象存储路径。 |
| `LOG_LEVEL` | `info` | 可设为 `debug`、`info`、`warn`、`error`。 |
| `VERSION_RETENTION_MIN_VERSIONS` | `20` | 每个文件至少保留的历史版本数量。 |
| `VERSION_RETENTION_MAX_AGE_DAYS` | `30` | 历史版本最大保留天数；设为 `0` 可禁用按年龄清理。 |

如果服务暴露到公网，建议放在 HTTPS 反向代理后面，并限制管理端口访问。

## 3. 安装 CLI

Release 附带以下辅助二进制包：

- `synchub-0.1.1-linux-amd64.tar.gz`
- `synchub-0.1.1-linux-arm64.tar.gz`
- `synchub-0.1.1-windows-amd64.zip`

### 3.1 Linux 客户端

```bash
mkdir -p ~/tmp/synchub-0.1.1
tar -xzf synchub-0.1.1-linux-amd64.tar.gz -C ~/tmp/synchub-0.1.1
sudo install -m 0755 ~/tmp/synchub-0.1.1/synchub-cli /usr/local/bin/synchub-cli
synchub-cli version
```

### 3.2 Windows 客户端

PowerShell 示例：

```powershell
$installDir = "$env:USERPROFILE\bin\synchub"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
Expand-Archive .\synchub-0.1.1-windows-amd64.zip -DestinationPath $installDir -Force
$env:Path = "$installDir;$env:Path"
synchub-cli version
```

如需长期使用，把 `$installDir` 添加到用户 `PATH`。

### 3.3 从源码运行

开发者也可以在项目根目录直接运行：

```powershell
go run .\cmd\synchub-cli version
```

下文命令默认使用已安装的 `synchub-cli`。如果从源码运行，把命令前缀替换为 `go run .\cmd\synchub-cli`。

## 4. 账号登录

设置服务端地址：

```powershell
$env:SYNCHUB_SERVER = "http://your-server:8765"
```

注册新用户：

```powershell
synchub-cli register `
  --server $env:SYNCHUB_SERVER `
  --email user@example.com `
  --password "change-me"
```

已有用户登录：

```powershell
synchub-cli login `
  --server $env:SYNCHUB_SERVER `
  --email user@example.com `
  --password "change-me"
```

登录成功后，CLI 会写入登录配置。默认路径由操作系统决定：

| 系统 | 默认登录配置 |
| --- | --- |
| Windows | `%AppData%\SyncHub\config.json` |
| Linux | `$XDG_CONFIG_HOME/SyncHub/config.json` 或 `$HOME/.config/SyncHub/config.json` |
| macOS | `$HOME/Library/Application Support/SyncHub/config.json` |

可以用环境变量或参数改写配置路径：

```powershell
$env:SYNCHUB_CONFIG = "F:\secure\synchub-login.json"
synchub-cli login --server $env:SYNCHUB_SERVER --email user@example.com --password "change-me"
```

或：

```powershell
synchub-cli login --config .\.synchub-login.json --server $env:SYNCHUB_SERVER --email user@example.com --password "change-me"
```

退出登录会撤销 refresh token 并删除本地登录配置：

```powershell
synchub-cli logout
```

## 5. 初始化工作区

创建或选择一个要同步的目录：

```powershell
$workspace = "F:\work\notes"
New-Item -ItemType Directory -Force -Path $workspace | Out-Null
```

初始化工作区，并把它映射到远端路径 `/notes`：

```powershell
synchub-cli workspace init --path $workspace --remote-path /notes
```

如果要一次初始化多个目录，可以把多个本地路径放在同一个命令里，并用 `--remote-root` 指定共同的远端父路径。下面会分别生成 `/workspace/notes` 和 `/workspace/code`：

```powershell
synchub-cli workspace init --remote-root /workspace F:\work\notes D:\work\code
```

初始化后会生成：

```text
<workspace>/.synchub/workspace.json
```

初始化时还会把该目录写入用户级 workspace registry。以后系统登录后只要启动一次 daemon，它就会读取 registry 并监听所有已初始化 workspace，不依赖当前目录：

```powershell
synchub-cli sync daemon
```

建议先运行诊断：

```powershell
synchub-cli sync doctor --path $workspace
```

如果输出里只有 `warn`，通常表示还没有执行过第一次同步。按 `next` 建议继续即可。如果出现 `fail`，先修复登录、网络或工作区配置。

## 6. 手动同步

### 6.1 预览同步计划

在真正上传或下载前，先 dry run：

```powershell
synchub-cli sync once --path $workspace --dry-run
```

### 6.2 执行一次完整同步

```powershell
synchub-cli sync once --path $workspace --device-name laptop --platform windows
```

`sync once` 会先上传本地新增、修改、删除和移动，再拉取远端变化。

### 6.3 查看工作区状态

```powershell
synchub-cli sync status --path $workspace
```

查看远端待拉取变更和远端冲突：

```powershell
synchub-cli sync status --path $workspace --show-remote --show-conflicts
```

机器可读输出：

```powershell
synchub-cli sync status --path $workspace --json
```

### 6.4 分开执行 push 和 pull

只上传本地变化：

```powershell
synchub-cli sync push --path $workspace --dry-run
synchub-cli sync push --path $workspace
```

只拉取远端变化：

```powershell
synchub-cli sync pull --path $workspace --dry-run
synchub-cli sync pull --path $workspace
```

如果本地游标过旧，按错误提示重放可用变更：

```powershell
synchub-cli sync pull --path $workspace --reset-cursor
```

## 7. 多设备同步

在第二台设备上重复以下步骤：

1. 安装 `synchub-cli`。
2. 登录同一个 SyncHub 账号。
3. 初始化一个本地目录，并使用同一个 `--remote-path`。
4. 执行 `sync once`。

示例：

```powershell
$workspace = "D:\work\notes"
synchub-cli login --server http://your-server:8765 --email user@example.com --password "change-me"
synchub-cli workspace init --path $workspace --remote-path /notes
synchub-cli sync once --path $workspace --device-name desktop --platform windows
```

查看已注册设备：

```powershell
synchub-cli sync devices --path $workspace
synchub-cli sync devices --path $workspace --json
```

## 8. 后台同步 Daemon

Daemon 是 `synchub-cli` 内置的后台同步模式，适合日常持续同步；不再需要单独安装或启动 `synchub-agent`。

日常使用只需要启动一次 daemon。它会读取用户级 workspace registry，并监听所有已初始化 workspace：

```powershell
synchub-cli sync daemon
```

这个命令适合放进系统开机自启或用户登录自启任务。Daemon 默认监听本地变化，有变化时尽快触发同步，同时按 `--interval` 做兜底同步。

只操作某个 workspace 时，再显式传入 `--path`：

```powershell
synchub-cli sync daemon --path $workspace
```

执行一次 Daemon 同步：

```powershell
synchub-cli sync daemon --path $workspace --once
```

预览一次 Daemon 同步：

```powershell
synchub-cli sync daemon --path $workspace --once --dry-run
```

调整同步周期和设备信息：

```powershell
synchub-cli sync daemon --path $workspace --interval 30s --device-name laptop --platform windows
```

调整本地变化监听间隔：

```powershell
synchub-cli sync daemon --path $workspace --watch-interval 1s --interval 30s
```

如果只想按周期同步，不监听本地变化：

```powershell
synchub-cli sync daemon --path $workspace --no-watch --interval 30s
```

查看 Daemon 状态：

```powershell
synchub-cli sync daemon --path $workspace --status
synchub-cli sync daemon --path $workspace --status --json
```

暂停和恢复：

```powershell
synchub-cli sync daemon --path $workspace --pause
synchub-cli sync daemon --path $workspace --resume
```

清理 Daemon 状态文件：

```powershell
synchub-cli sync daemon --path $workspace --reset-state
```

Daemon 状态默认写入：

```text
<workspace>/.synchub/daemon-state.json
<workspace>/.synchub/daemon-control.json
```

## 9. 忽略不需要同步的文件

在工作区根目录创建 `.synchubignore`：

```text
# build output
dist/
bin/
*.log
node_modules/
.env
```

规则说明：

- 空行和 `#` 开头的行会被忽略。
- `dist/` 这类以 `/` 结尾的规则只匹配目录。
- `*.log` 会匹配任意路径段里的日志文件。
- 包含 `/` 的规则按工作区相对路径匹配，例如 `docs/*.tmp`。
- `.synchub` 目录不会被同步；`.synchubignore` 文件会像普通文件一样同步，方便多设备保持同一套忽略规则。

查看当前忽略规则：

```powershell
synchub-cli manifest ignores --path $workspace
```

只扫描 manifest，不上传：

```powershell
synchub-cli manifest scan --path $workspace --dry-run
```

## 10. 文件操作和版本历史

列出远端文件：

```powershell
synchub-cli file list --path $workspace
synchub-cli file list --path $workspace --remote-path /notes/docs
```

创建远端目录：

```powershell
synchub-cli file mkdir --path $workspace --remote-path /notes/docs
```

移动远端文件：

```powershell
synchub-cli file move --path $workspace --remote-path /notes/readme.md --to /notes/docs/readme.md
```

删除远端文件：

```powershell
synchub-cli file delete --path $workspace --remote-path /notes/docs/readme.md
```

下载远端文件：

```powershell
synchub-cli file download --path $workspace --remote-path /notes/docs/readme.md --output .\readme.md
```

查看版本历史：

```powershell
synchub-cli file versions --path $workspace --remote-path /notes/docs/readme.md
```

恢复指定版本：

```powershell
synchub-cli file restore --path $workspace --remote-path /notes/docs/readme.md --version 1
```

保护一个历史版本，避免被清理：

```powershell
synchub-cli file pin --path $workspace --remote-path /notes/docs/readme.md --version 1
synchub-cli file unpin --path $workspace --remote-path /notes/docs/readme.md --version 1
```

## 11. 冲突和本地 Trash

当本地和远端都修改了同一个文件时，SyncHub 不会静默覆盖本地内容。

常见结果：

- 本地文件被保留为 `name.conflict-<device>-<timestamp>.ext`。
- 远端冲突副本会使用类似的 `conflict` 命名。
- 远端删除同步到本地时，本地文件会移动到 `.synchub/trash`。

查看冲突：

```powershell
synchub-cli sync conflicts --path $workspace
```

将冲突标记为保留双方：

```powershell
synchub-cli sync conflicts resolve --path $workspace --id conf_1 --resolution keep_both
```

查看本地 trash：

```powershell
synchub-cli sync trash --path $workspace
```

从 trash 恢复：

```powershell
synchub-cli sync trash restore --path $workspace --batch 20260702T010000.000000000Z --entry docs/readme.md
```

## 12. 备份和恢复

### 12.1 Docker 服务端数据

Compose 部署默认使用 `synchub-data` volume。备份前建议停止写入或短暂停服：

```bash
docker compose -f docker-compose.release.yml stop synchub-api
docker run --rm \
  -v synchub_synchub-data:/data \
  -v "$PWD:/backup" \
  alpine:3.22 \
  tar -czf /backup/synchub-data-backup.tar.gz -C /data .
docker compose -f docker-compose.release.yml up -d
```

恢复时先停服，再把备份解压回 volume。恢复前请确认目标 volume 可以被覆盖。

### 12.2 本地开发数据

如果使用源码模式运行服务端，默认数据目录是 `.data`：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\backup-local.ps1 -DataDir .data -OutputDir .backups
```

恢复：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\restore-local.ps1 -BackupPath .backups\synchub-local-YYYYMMDD-HHMMSS.zip -DataDir .data
```

如果目标目录已存在并确认覆盖：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\restore-local.ps1 -BackupPath .backups\synchub-local-YYYYMMDD-HHMMSS.zip -DataDir .data -Force
```

## 13. 升级

升级 Docker 部署：

```bash
cd ~/synchub
export SYNCHUB_IMAGE=ghcr.io/bruceblink/synchub:<new-version>
export JWT_SECRET='replace-with-a-long-random-secret'
docker compose -f docker-compose.release.yml pull
docker compose -f docker-compose.release.yml up -d
curl -fsS http://127.0.0.1:8765/readyz
```

升级 CLI 时，下载新版本 Release archive，替换原来的 `synchub-cli` 即可。

## 14. 本地开发模式

开发环境可以不使用 Docker，直接运行 API：

```powershell
$env:HTTP_ADDR = ":8765"
$env:DATABASE_DRIVER = "sqlite"
$env:DATABASE_URL = ".\.data\synchub.db"
$env:STORAGE_BACKEND = "local"
$env:LOCAL_STORAGE_ROOT = ".\.data\storage"
$env:JWT_SECRET = "local-dev-secret"
go run .\cmd\synchub-api
```

另开一个 PowerShell 窗口检查：

```powershell
go run .\cmd\synchub-cli server wait --server http://localhost:8765 --timeout 30s
go run .\cmd\synchub-cli server status --server http://localhost:8765
```

本地 Docker 构建和 smoke test：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.1.1 -Image synchub:0.1.1
```

本地 Compose 开发服务：

```powershell
$env:SYNCHUB_VERSION = "0.1.1"
$env:SYNCHUB_IMAGE = "synchub:0.1.1"
$env:GOPROXY = "https://goproxy.cn,direct"
docker compose up --build
```

## 15. 服务检查和 API 文档

服务端常用端点：

| 端点 | 说明 |
| --- | --- |
| `GET /version` | 当前服务名称和版本。 |
| `GET /healthz` | 进程存活检查。 |
| `GET /readyz` | 数据库和存储可用性检查。 |
| `GET /metrics` | Prometheus text format 指标。 |
| `GET /swagger/` | Swagger UI。 |
| `GET /swagger/openapi.yaml` | OpenAPI YAML。 |

CLI 检查命令：

```powershell
synchub-cli server wait --server http://your-server:8765 --timeout 30s
synchub-cli server status --server http://your-server:8765
synchub-cli server metrics --server http://your-server:8765
synchub-cli server openapi --server http://your-server:8765 --output .\openapi.yaml
```

## 16. 常见问题

### 16.1 `not logged in`

先登录：

```powershell
synchub-cli login --server http://your-server:8765 --email user@example.com --password "change-me"
```

如果使用了自定义配置路径，后续命令也要带上同一个 `--config`，或设置 `SYNCHUB_CONFIG`。

### 16.2 `workspace is not initialized`

先初始化工作区：

```powershell
synchub-cli workspace init --path $workspace --remote-path /notes
```

### 16.3 同步前不确定会发生什么

先 dry run：

```powershell
synchub-cli sync once --path $workspace --dry-run
synchub-cli sync status --path $workspace --show-remote --show-conflicts
```

### 16.4 本地文件被删除了

先查看 trash：

```powershell
synchub-cli sync trash --path $workspace
```

如果是远端删除同步导致，本地文件通常可以从 `.synchub/trash` 恢复。

### 16.5 出现冲突文件

冲突文件表示 SyncHub 检测到两端同时修改，不会自动覆盖。检查冲突副本内容后，保留需要的版本，再执行一次同步。

```powershell
synchub-cli sync conflicts --path $workspace
```

### 16.6 Docker 镜像拉取失败

优先检查：

- Linux 服务器是否可以访问 `ghcr.io`。
- GHCR package 是否需要 `docker login ghcr.io`。
- 防火墙或代理是否允许 Docker 拉取镜像。

### 16.7 Docker build 在 `golang:*` 或 `alpine:*` 卡住

这通常是 Docker Hub 或代理网络问题，不是项目编译失败。可以在本地构建时覆盖基础镜像源：

```powershell
$env:GO_IMAGE = "golang:1.26-alpine"
$env:RUNTIME_IMAGE = "alpine:3.22"
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-docker-image.ps1 -Version 0.1.1 -Image synchub:0.1.1
```

国内网络也可以通过 Docker Desktop registry mirror、`GOPROXY` 或构建参数调整。

## 17. 命令速查

账号：

```powershell
synchub-cli register --server http://your-server:8765 --email user@example.com --password "change-me"
synchub-cli login --server http://your-server:8765 --email user@example.com --password "change-me"
synchub-cli logout
```

工作区：

```powershell
synchub-cli workspace init --path $workspace --remote-path /notes
synchub-cli sync doctor --path $workspace
synchub-cli sync status --path $workspace
```

同步：

```powershell
synchub-cli sync once --path $workspace --dry-run
synchub-cli sync once --path $workspace
synchub-cli sync push --path $workspace
synchub-cli sync pull --path $workspace
```

Daemon：

```powershell
synchub-cli sync daemon
synchub-cli sync daemon --path $workspace
synchub-cli sync daemon --path $workspace --once
synchub-cli sync daemon --path $workspace --status
synchub-cli sync daemon --path $workspace --pause
synchub-cli sync daemon --path $workspace --resume
```

文件：

```powershell
synchub-cli file list --path $workspace
synchub-cli file download --path $workspace --remote-path /notes/readme.md --output .\readme.md
synchub-cli file versions --path $workspace --remote-path /notes/readme.md
synchub-cli file restore --path $workspace --remote-path /notes/readme.md --version 1
```

状态和排查：

```powershell
synchub-cli server status --server http://your-server:8765
synchub-cli sync devices --path $workspace
synchub-cli sync conflicts --path $workspace
synchub-cli sync trash --path $workspace
```
