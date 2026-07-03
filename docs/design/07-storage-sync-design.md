# 同步与存储设计

## 存储模型

文件内容通过 Storage interface 写入对象存储，数据库只保存元数据和对象 key。

首期 Local FS key 规则：

```text
objects/{user_id}/{sha256[0..2]}/{sha256}
staging/{user_id}/{upload_id}/{chunk_index}
```

后续 S3-compatible backend 保持同样 key 语义。

## 分片上传

- 默认 chunk size: 4 MiB。
- 客户端可请求 chunk size，但服务端可以按配置修正。
- 每个 chunk 必须携带 sha256，服务端写入后记录 checksum。
- commit 时校验 chunk 数量、大小、顺序和完整文件 sha256。
- 上传会话默认 TTL: 24 小时。
- 过期 staging object 由后台任务清理。

## 断点续传

客户端可通过 `GET /uploads/{upload_id}` 查询已上传 chunk 列表，然后只补传缺失分片。

服务端幂等规则：

- 同一 `upload_id + chunk_index` 重传且 checksum 一致，返回成功。
- checksum 不一致，返回 `UPLOAD_CHECKSUM_MISMATCH`。
- committed session 不允许再次写 chunk。

## 下载

- 文件下载必须支持 streaming。
- 支持 `Range`，用于断点下载和 WebDAV 客户端。
- `ETag` 使用文件版本和 sha256 生成。

## 同步策略

同步引擎以 change_events 为主线，hash diff 为补偿：

- 正常增量同步：设备提交 `last_applied_change_id`，服务端返回之后的 change_events。
- 初次同步或游标失效：客户端上传本地 manifest，服务端基于 path、sha256、version 生成 diff。
- 同一路径若服务端版本已前进且客户端基于旧版本提交，进入冲突处理。

## 冲突策略

Phase 1 不做内容级合并。Phase 2 使用 keep-both 默认策略：

- 远端文件保持原路径。
- 本地冲突版本上传为 `{name}.conflict-{device}-{timestamp}{ext}`。
- 记录 sync_conflicts，后续由 CLI、Agent API 或任意客户端提示用户处理。

不推荐长期使用 last-write-wins 作为默认策略，因为它会静默覆盖工作区文件。只有用户显式配置某些目录为 LWW 时才启用。

## 删除语义

- 删除先写入 soft delete 和 change_event。
- Storage 对象不立即删除；当前 MVP 先清理过期历史版本记录，孤儿对象扫描留到 Later。
- 客户端收到 delete event 后移入本地回收区或按配置删除。

## 版本保留

默认策略：

- 保留最近 20 个版本。
- 保留最近 30 天内的版本。
- 手动 pin 的版本不自动清理。
- 可通过 `VERSION_RETENTION_MIN_VERSIONS` 和 `VERSION_RETENTION_MAX_AGE_DAYS` 调整。

Phase 3 的 worker 会清理过期历史版本记录，但不会删除当前版本、pinned version 和每个文件最新保留窗口内的版本。
