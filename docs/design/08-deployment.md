# 部署设计

## 首期部署形态
Phase 1 使用 Docker Compose 部署：
- `synchub-api`
- `postgres`
- `local-storage` volume

Phase 2 起按需要增加：
- `redis`
- `synchub-worker`
- `minio`（本地模拟 S3-compatible storage）

## 环境变量
必需：
- `DATABASE_URL`
- `JWT_SECRET`
- `STORAGE_BACKEND`
- `LOCAL_STORAGE_ROOT`

可选：
- `LOG_LEVEL`
- `HTTP_ADDR`
- `UPLOAD_CHUNK_SIZE`
- `UPLOAD_SESSION_TTL_SECONDS`
- `REDIS_URL`
- `S3_ENDPOINT`
- `S3_BUCKET`
- `S3_REGION`
- `S3_ACCESS_KEY_ID`
- `S3_SECRET_ACCESS_KEY`

## 数据卷
- PostgreSQL 数据卷必须持久化。
- Local FS storage root 必须持久化。
- staging storage 可以和 object storage 放在同一 volume，但需要后台清理策略。

## 发布流程
1. 构建镜像。
2. 运行 migration。
3. 滚动替换 API container。
4. 健康检查通过后开放流量。

## 健康检查
- `GET /healthz`: 进程存活。
- `GET /readyz`: 数据库、storage 可用。

## 备份
- PostgreSQL 定期备份。
- Local FS storage 按对象目录备份。
- 数据库和 storage 备份需要时间点接近，否则恢复后可能出现孤儿对象；孤儿对象由修复任务处理。
