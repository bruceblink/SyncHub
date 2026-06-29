# API 规范

## 基础约定
- Base path: `/api/v1`
- Auth: `Authorization: Bearer <access_token>`
- Content-Type: JSON 接口使用 `application/json`；chunk 上传使用 `application/octet-stream` 或 multipart。
- Pagination: `page_size` 最大 200，列表接口返回 `next_cursor`。
- Idempotency: 创建上传会话、提交上传、删除文件应支持 `Idempotency-Key`。

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

### 创建目录
`POST /api/v1/files/directories`

Request:
```json
{
  "parent_id": "root",
  "name": "notes"
}
```

### 移动 / 重命名
`PATCH /api/v1/files/{file_id}`

Request:
```json
{
  "parent_id": "new_parent_id",
  "name": "new-name.txt"
}
```

### 删除
`DELETE /api/v1/files/{file_id}`

## Upload

### 上传初始化
`POST /api/v1/uploads`

Request:
```json
{
  "path": "/workspace/a.txt",
  "size": 10485760,
  "sha256": "hex",
  "chunk_size": 4194304,
  "base_version": 3
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

### 心跳
`POST /api/v1/devices/{device_id}/heartbeat`

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

## WebDAV
WebDAV adapter 后续映射到相同 file service：
- `PROPFIND` -> list / metadata
- `GET` -> download
- `PUT` -> upload
- `MOVE` -> move
- `DELETE` -> delete
