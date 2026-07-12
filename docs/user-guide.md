# SyncHub 使用指南

SyncHub 由服务端和 SyncHub Desktop 两部分组成。服务端提供 REST API、PostgreSQL 元数据和对象存储；桌面应用是唯一面向用户的同步客户端，不需要安装 CLI。

## 1. 启动服务端

服务端在所有环境中都要求 PostgreSQL：

```powershell
$env:DATABASE_URL = "postgresql://user:password@host:5432/synchub?sslmode=require"
$env:JWT_SECRET = "replace-with-a-long-random-secret"
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\build-web-admin.ps1
go run .\cmd\synchub-api
```

默认地址为 `http://localhost:8765`。访问根路径会跳转到管理页面；运行状态可通过以下端点检查：

- `/version`
- `/healthz`
- `/readyz`
- `/metrics`
- `/swagger/`

生产环境可使用 Docker、`docker-compose.release.yml` 或 `fly.toml` 部署。数据库迁移会在 API 启动时自动执行。

## 2. 启动桌面客户端

桌面端仓库位于 `F:\project\synchub-desktop`。Windows 需要 Visual Studio 2022 MSVC 工具链：

```powershell
cd F:\project\synchub-desktop
cargo run
```

桌面应用直接调用服务端 API，不会查找或启动任何 CLI 可执行文件。

## 3. 登录与工作区

1. 在顶部输入服务端地址，例如 `https://sync.likanug.app`，然后保存。
2. 使用邮箱和密码登录或注册。
3. 在侧边栏输入一个或多个本地目录；多个目录可用换行或分号分隔。
4. 可填写远端根目录，然后点击初始化。

已有旧版本登录配置和工作区 registry 会被自动读取，以便无损升级。桌面设置中的服务端地址是最终权威值，修改后会同步更新已注册工作区。

## 4. 日常同步

已登录时，桌面应用会自动为所有注册工作区启动后台同步。Sync 页面还提供：

- `Sync Once`：立即执行完整 push + pull。
- `Dry Run`：预览本地变化，不修改远端。
- `Push` / `Pull`：单独执行上传或拉取。
- `Status`：显示新增、修改和删除数量。
- `Doctor`：检查工作区、登录、服务端、设备和 manifest。

Daemon 页面可查看后台任务状态，并执行暂停、恢复和状态重置。退出登录会停止当前进程中的所有后台同步任务。

在工作区根目录创建 `.synchubignore` 可排除构建产物等文件。`.synchub` 元数据目录始终不会同步。

## 5. 文件与版本

桌面端可直接完成以下操作：

- 浏览远端文件并分页加载。
- 创建目录、移动、重命名和删除文件。
- 下载文件到对应本地工作区路径。
- 查看、恢复、固定和取消固定历史版本。
- 查看本地保护性回收站和云端回收站并恢复内容。

远端删除会先把本地内容移动到 `.synchub/trash`。如果本地文件相对上次同步已有修改，拉取会保留 conflict 副本，避免静默覆盖。

## 6. 设备与冲突

Devices 页面显示注册设备及当前设备。首次完整同步会自动注册设备，后续同步会发送心跳和结果状态。

Conflicts 页面可选择保留本地、保留远端或两者都保留。处理后刷新列表确认冲突已消失。

## 7. 验证与排查

服务端验证：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-mvp.ps1
```

桌面端验证：

```powershell
cd F:\project\synchub-desktop
cargo fmt -- --check
cargo test
cargo check
cargo build --release
```

常见排查顺序：

1. 确认 `/readyz` 的 database 和 storage 都为 ready。
2. 在桌面 Server 页面刷新状态。
3. 在 Sync 页面运行 Doctor。
4. 在 Daemon 页面查看最近错误和失败次数。
5. 检查工作区 `.synchub/manifest.json`、`daemon-state.json` 和 `daemon-control.json`。

## 8. 升级

服务端升级时替换 API 镜像或服务端二进制，并保留 PostgreSQL 与对象存储。桌面端升级时替换桌面应用即可；现有登录、工作区、manifest、同步游标和回收站数据会继续使用。
