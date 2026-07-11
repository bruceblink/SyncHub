# API 规范

## 基础约定

- Base path: `/api/v1`
- Auth: `Authorization: Bearer <access_token>`
- Content-Type: JSON 接口使用 `application/json`；chunk 上传使用 `application/octet-stream` 或 multipart。
- Pagination: `page_size` 最大 200，列表接口返回 `next_cursor`。
- Idempotency: 创建上传会话、提交上传、删除文件应支持 `Idempotency-Key`。
- Observability: `GET /healthz`、`GET /readyz`、`GET /metrics`、`GET /swagger/openapi.yaml` 不需要认证。

## 通用响应

成功：

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

失败：

```json
{
  "code": "FILE_NOT_FOUND",
  "message": "file not found",
  "trace_id": "01J...",
  "details": {}
}
```

## 错误码分类

- `AUTH_INVALID_CREDENTIALS`
- `AUTH_TOKEN_EXPIRED`
- `AUTH_PERMISSION_DENIED`
- `FILE_NOT_FOUND`
- `FILE_CONFLICT`
- `UPLOAD_SESSION_EXPIRED`
- `UPLOAD_CHECKSUM_MISMATCH`
- `STORAGE_QUOTA_EXCEEDED`
- `SYNC_CURSOR_EXPIRED`
- `INTERNAL_ERROR`

## Auth

### 注册

`POST /api/v1/auth/register`

Request:

```json
{
  "email": "user@example.com",
  "password": "password"
}
```

### 登录

`POST /api/v1/auth/login`

Response data:

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_in": 900
}
```

### 刷新 token

`POST /api/v1/auth/refresh`

### 登出

`POST /api/v1/auth/logout`

## File

### 获取文件或目录信息

`GET /api/v1/files/{file_id}`

### 按路径查询

`GET /api/v1/files/by-path?path=/workspace/a.txt`

### 列出目录

`GET /api/v1/files?parent_id={id}&cursor={cursor}&page_size=100`

`cursor` 使用上一页响应的 `next_cursor`；没有下一页时 `next_cursor` 为空。

### 创建目录

`POST /api/v1/files/directories`

Request:

```json
{
  "path": "/workspace/notes",
  "device_id": "dev_..."
}
```

### 移动 / 重命名

`PATCH /api/v1/files/{file_id}`

Request:

```json
{
  "path": "/workspace/new-name.txt",
  "device_id": "dev_..."
}
```

### 删除

`DELETE /api/v1/files/{file_id}`

Request:

```json
{
  "device_id": "dev_..."
}
```

### 版本历史

`GET /api/v1/files/{file_id}/versions?limit=100`

### 恢复版本

`POST /api/v1/files/{file_id}/versions/{version}/restore`

Request:

```json
{
  "device_id": "dev_..."
}
```

### Pin / Unpin 版本

```text
POST   /api/v1/files/{file_id}/versions/{version}/pin
DELETE /api/v1/files/{file_id}/versions/{version}/pin
```

## Upload

### 上传初始化

`POST /api/v1/uploads`

Headers:

- `Idempotency-Key: <key>` 可选；同一用户使用相同 key 重试时返回同一个上传会话。

Request:

```json
{
  "path": "/workspace/a.txt",
  "size": 10485760,
  "sha256": "hex",
  "chunk_size": 4194304,
  "base_version": 3,
  "device_id": "dev_..."
}
```

Response data:

```json
{
  "upload_id": "upl_...",
  "chunk_size": 4194304,
  "expires_at": "2026-06-29T12:00:00Z",
  "uploaded_chunks": []
}
```

### 上传分片

`PUT /api/v1/uploads/{upload_id}/chunks/{chunk_index}`

Headers:

- `Content-Type: application/octet-stream`
- `X-Chunk-Sha256: <hex>`

### 查询上传状态

`GET /api/v1/uploads/{upload_id}`

### 取消上传

`DELETE /api/v1/uploads/{upload_id}`

将待处理会话标记为 `aborted`，并回收已经上传的暂存分片。重复取消同一个会话是幂等的；已提交或过期的会话不能取消。

### 提交上传

`POST /api/v1/uploads/{upload_id}/commit`

Response data:

```json
{
  "file_id": "file_...",
  "version": 4,
  "change_id": 1024
}
```

## Download

### 下载文件

`GET /api/v1/files/{file_id}/content`

支持：

- `Range` header
- `ETag`
- `If-None-Match`

## Sync

### 注册设备

`POST /api/v1/devices`

Request:

```json
{
  "name": "work-laptop",
  "platform": "windows"
}
```

### 设备列表

`GET /api/v1/devices?limit=100`

返回当前用户已注册设备及其 `last_seen_at`、`last_applied_change_id`，用于排查多设备同步游标和最近在线状态。

### 心跳

`POST /api/v1/devices/{device_id}/heartbeat`

### 撤销设备

`DELETE /api/v1/devices/{device_id}`

仅可撤销当前用户自己的设备。撤销后该设备无法继续发送心跳、拉取变更或确认同步游标，客户端需要重新注册设备后才能恢复同步。

### 活动记录

`GET /api/v1/activity?file_id={id}&before_event_id={id}&limit=50`

返回当前用户的文件操作记录，按最新优先排列。`file_id` 可选，用于查看单个文件的操作时间线；`before_event_id` 用于继续加载更早记录，不需要注册同步设备。

### 拉取变更

`GET /api/v1/sync/changes?device_id={id}&after_change_id={id}&limit=500`

### 提交同步结果

`POST /api/v1/sync/ack`

Request:

```json
{
  "device_id": "dev_...",
  "last_applied_change_id": 1024
}
```

### 查询冲突

`GET /api/v1/sync/conflicts?resolution=pending&limit=100`

### 标记冲突处理结果

`PATCH /api/v1/sync/conflicts/{conflict_id}`

Request:

```json
{
  "resolution": "keep_both"
}
```

## Later: WebDAV

WebDAV adapter 只作为长期扩展记录，不属于当前 MVP。后续如恢复该方向，再映射到相同 file service：

- `PROPFIND` -> list / metadata
- `GET` -> download
- `PUT` -> upload
- `MOVE` -> move
- `DELETE` -> delete
