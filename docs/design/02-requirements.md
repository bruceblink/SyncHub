# 需求分析

## 目标用户

- 在多台机器之间同步工作区文件的开发者。
- 需要同步 Codex、Claude Code、Cursor、VS Code、Obsidian 等工具配置和会话上下文的重度工具用户。
- 希望通过 WebDAV 或 HTTP API 接入私有同步服务的个人或小团队。

## Phase 1 功能需求

- 用户注册、登录、refresh token、登出。
- 创建、查询、移动、删除文件或目录元数据。
- 文件上传 / 下载。
- 分片上传、断点续传、上传会话过期清理。
- 文件 hash、size、mime、版本号和 storage key 管理。
- 基于用户维度的存储隔离。
- Local FS storage backend。
- 基础审计字段：created_at、updated_at、deleted_at。

## Phase 2 功能需求

- Agent 设备注册与心跳。
- 文件监听与变更上报。
- 基于 hash / version / change_id 的增量同步。
- 冲突检测与冲突文件命名策略。
- 同步游标和设备同步状态。

## Phase 3+ 功能需求

- 文件版本历史查询与恢复。
- WebDAV adapter。
- CLI 操作：login、sync、status、pull、push。
- 稳定同步协议与 Agent SDK，供任意 GUI、Web、移动端或第三方客户端适配。
- S3 / OSS / MinIO storage backend。
- 团队空间与共享目录。

## 非功能需求

- 正确性：文件内容、元数据和版本关系必须可校验，commit 操作需要幂等。
- 性能：单实例支持百万级文件元数据；上传下载使用 streaming；常用列表查询需要分页和索引。
- 同步延迟：局域网目标秒级，公网场景以最终一致为主。
- 可用性：早期目标为单实例可恢复，生产目标 99.9%。
- 安全：用户级数据隔离；token 可撤销；密码哈希不可逆；敏感配置只通过环境变量注入。
- 可观测性：关键请求、上传会话、同步任务、错误链路需要 trace id。
- 可运维：支持 Docker Compose 本地部署；迁移脚本可重复执行。

## 暂不支持

- 多人实时协作编辑。
- 文件内容语义合并。
- 大规模企业权限模型。
- 富媒体转码、预览和搜索索引。
