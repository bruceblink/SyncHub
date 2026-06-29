# 安全设计

## 认证策略
Phase 1 使用邮箱密码 + JWT：
- Password hash: Argon2id。
- Access token: JWT，短有效期，建议 15 分钟。
- Refresh token: 随机高熵 token，只在数据库保存 hash，支持撤销。
- Logout: 撤销 refresh token，access token 等待自然过期。

OAuth2 作为 Phase 3+ 登录扩展，不阻塞 MVP。

## 授权模型
- 所有文件、版本、上传会话、设备、变更日志必须绑定 `user_id`。
- API handler 只能通过认证 extractor 获取当前用户。
- Repository 查询必须带 `user_id` 条件，禁止只按资源 ID 查询用户数据。
- WebDAV adapter 复用相同授权逻辑。

## Token Claims
Access token 建议包含：
- `sub`: user id
- `exp`: 过期时间
- `iat`: 签发时间
- `jti`: token id

不要在 token 中放入密码哈希、refresh token、存储凭证等敏感数据。

## 密钥与配置
- `JWT_SECRET` 只从环境变量或密钥管理系统读取。
- 生产环境不得使用默认 secret。
- S3 / OSS 访问密钥不得写入配置文件或日志。
- 日志中需要脱敏 Authorization、Cookie、refresh token。

## 防护
- 登录、注册、refresh 接口需要 rate limit。
- upload init / commit 需要幂等键，减少重试导致的重复写入。
- chunk 上传校验 sha256，commit 校验完整文件 sha256。
- 下载接口校验权限后再返回 Range 内容。
- 对 path 做规范化，拒绝 `..`、绝对系统路径、空字节和非法分隔符。

## 服务间通信
Phase 1 为单进程模块化单体，不需要服务间认证。

拆分 worker 或服务后：
- 内部调用使用 service token。
- 跨主机内部通信可启用 mTLS。
- service token 需要独立轮换机制，不能复用用户 JWT secret。

## 审计
需要记录：
- 登录成功 / 失败。
- refresh token 撤销。
- 文件 create / update / move / delete / restore。
- 冲突创建和解决。
- 管理类配置变更。

审计日志中不记录文件内容和敏感 token。
