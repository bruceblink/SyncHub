# SyncHub 项目总览

## 项目定位
SyncHub 是一个面向多端的分布式文件同步系统，提供类似 WebDAV 的访问能力，并支持增量同步与版本管理。

## 核心能力
- 多端文件同步（PC / Mobile / Web）
- WebDAV / HTTP API 支持
- 分片上传与断点续传
- 增量同步机制
- 文件版本管理
- 多用户隔离存储

## 技术栈
- Backend: Rust (Axum / Actix)
- Storage: Local FS / S3-compatible storage
- DB: PostgreSQL (sqlx)
- Auth: OAuth2 + JWT
- Protocol: HTTP / WebDAV

## 系统边界
不负责：
- 在线文档协作编辑
- 富媒体处理（仅存储）
