# API 规范

## 通用结构
```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

## 接口

### 上传初始化
POST /api/v1/upload/init

### 上传分片
POST /api/v1/upload/chunk

### 提交上传
POST /api/v1/upload/commit

### 下载文件
GET /api/v1/file/{id}
