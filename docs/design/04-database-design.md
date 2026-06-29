# 数据库设计

## 数据库选择

开发阶段默认使用 SQLite，让本地运行、测试和单机 MVP 不依赖外部数据库服务。数据库访问必须经过 repository 边界，业务层不直接绑定具体 SQL 方言。

后续按部署规模增加 PostgreSQL / MySQL adapter。PostgreSQL 侧优先使用 pgx + sqlc；MySQL 侧优先使用 database/sql 或 sqlc 支持的 MySQL 查询。正式 migration 工具可使用 golang-migrate 或 goose。

## sqlc 与 GORM 取舍

首期 SQLite repository 使用手写 SQL 保持启动成本低。大型关系型数据库 adapter 默认选择 sqlc + 原生 driver，不把 GORM 作为核心数据访问层。

原因：

- SyncHub 的关键路径不是普通 CRUD，而是 upload commit 事务、乐观锁、部分唯一索引、游标分页、change feed、冲突检测和后台清理任务。
- 这些路径需要明确控制 SQL、事务隔离、锁、索引命中和返回字段，手写 SQL 更直接。
- sqlc 能保留 SQL 可读性，同时生成 Go 类型，减少手写 scan 和字段映射错误。
- PostgreSQL 适合后续使用 `FOR UPDATE`、`RETURNING`、批量写入、copy、事务和连接池能力。

GORM 可作为可选工具用于后台管理、低风险 CRUD 或原型验证，但不建议进入文件同步、版本、上传提交和 change_events 等核心路径。

## ID 与时间

- 主键建议使用 UUID 或 ULID。当前 SQLite 开发库使用 text 保存 UUID，后续 PostgreSQL 可映射为 uuid 类型，MySQL 可映射为 char(36) 或 binary(16)。
- 所有业务表包含 `created_at`、`updated_at`。
- 软删除资源包含 `deleted_at`。
- 对外暴露 ID 时保持字符串格式，不暴露自增序列。

## 核心表

### users

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | 用户 ID |
| email | citext unique | 登录邮箱 |
| password_hash | text | Argon2id 哈希 |
| status | text | active / disabled |
| created_at | timestamptz | 创建时间 |
| updated_at | timestamptz | 更新时间 |

### refresh_tokens

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | token ID |
| user_id | uuid fk | 用户 ID |
| token_hash | text unique | refresh token 哈希 |
| expires_at | timestamptz | 过期时间 |
| revoked_at | timestamptz nullable | 撤销时间 |
| created_at | timestamptz | 创建时间 |

### devices

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | 设备 ID |
| user_id | uuid fk | 用户 ID |
| name | text | 设备名称 |
| platform | text | windows / macos / linux / ios / android |
| last_seen_at | timestamptz | 最近心跳 |
| last_applied_change_id | bigint | 同步游标 |
| created_at | timestamptz | 创建时间 |
| updated_at | timestamptz | 更新时间 |

### file_nodes

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | 文件或目录 ID |
| user_id | uuid fk | 用户 ID |
| parent_id | uuid nullable | 父目录 ID，根目录为空 |
| name | text | 文件名 |
| path | text | 规范化绝对路径 |
| node_type | text | file / directory |
| current_version_id | uuid nullable | 当前版本 |
| size | bigint | 当前文件大小 |
| sha256 | text nullable | 当前内容 hash |
| storage_key | text nullable | 当前对象指针 |
| version | bigint | 乐观锁版本号 |
| deleted_at | timestamptz nullable | 软删除时间 |
| created_at | timestamptz | 创建时间 |
| updated_at | timestamptz | 更新时间 |

约束：

- `(user_id, path)` 在 `deleted_at is null` 时唯一。
- `(user_id, parent_id, name)` 在 `deleted_at is null` 时唯一。
- 目录 `size` 为 0，`storage_key` 为空。

### file_versions

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | 版本 ID |
| file_id | uuid fk | 文件 ID |
| user_id | uuid fk | 用户 ID |
| version | bigint | 文件版本号 |
| size | bigint | 文件大小 |
| sha256 | text | 内容 hash |
| storage_key | text | storage 对象指针 |
| created_by_device_id | uuid nullable | 来源设备 |
| created_at | timestamptz | 创建时间 |

约束：

- `(file_id, version)` 唯一。
- `(user_id, sha256, size)` 可建普通索引用于去重或秒传扩展。

### upload_sessions

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | 上传会话 ID |
| user_id | uuid fk | 用户 ID |
| target_path | text | 目标路径 |
| target_file_id | uuid nullable | 已存在文件 ID |
| base_version | bigint nullable | 客户端基于的版本 |
| total_size | bigint | 总大小 |
| chunk_size | int | 分片大小 |
| sha256 | text | 完整文件 hash |
| status | text | pending / committed / expired / aborted |
| staging_key | text | 临时对象前缀 |
| expires_at | timestamptz | 过期时间 |
| idempotency_key | text nullable | 幂等键 |
| created_at | timestamptz | 创建时间 |
| updated_at | timestamptz | 更新时间 |

约束：

- `(user_id, idempotency_key)` 在 idempotency_key 非空时唯一。

### upload_chunks

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | chunk ID |
| upload_id | uuid fk | 上传会话 |
| chunk_index | int | 分片序号 |
| size | int | 分片大小 |
| sha256 | text | 分片 hash |
| storage_key | text | 临时对象指针 |
| created_at | timestamptz | 创建时间 |

约束：

- `(upload_id, chunk_index)` 唯一。

### change_events

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial pk | 单用户可排序 change id |
| user_id | uuid fk | 用户 ID |
| file_id | uuid | 文件 ID |
| event_type | text | create / update / move / delete / restore |
| version | bigint nullable | 文件版本 |
| path | text | 变更后的路径 |
| old_path | text nullable | move/delete 前路径 |
| source_device_id | uuid nullable | 来源设备 |
| created_at | timestamptz | 创建时间 |

索引：

- `(user_id, id)` 用于按游标拉取变更。
- `(user_id, file_id, created_at)` 用于文件历史。

### sync_conflicts

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | uuid pk | 冲突 ID |
| user_id | uuid fk | 用户 ID |
| file_id | uuid nullable | 原文件 ID |
| path | text | 冲突路径 |
| local_version | bigint nullable | 客户端版本 |
| remote_version | bigint nullable | 服务端版本 |
| resolution | text | pending / keep_local / keep_remote / keep_both |
| created_at | timestamptz | 创建时间 |
| resolved_at | timestamptz nullable | 解决时间 |

## 事务规则

- upload commit 必须在单个数据库事务中完成：锁定 upload session、校验 chunks、写入 file_nodes / file_versions、写入 change_events、标记 session committed。
- 文件 move / delete / restore 必须生成 change_events。
- 乐观锁使用 `file_nodes.version`，客户端提交的 `base_version` 低于当前版本时返回冲突。
- storage 对象写入和数据库事务无法天然原子，采用 staging -> commit -> finalize 模式；失败后由后台任务清理孤儿对象。

## 首期索引

- `users(email)`
- `devices(user_id, last_seen_at)`
- `file_nodes(user_id, parent_id, deleted_at)`
- `file_nodes(user_id, path) where deleted_at is null`
- `file_versions(file_id, version desc)`
- `upload_sessions(user_id, status, expires_at)`
- `upload_chunks(upload_id, chunk_index)`
- `change_events(user_id, id)`
