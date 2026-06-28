# 架构设计

## 总体架构
SyncHub 采用模块化单体 / 可演进微服务架构。

组件：
- API Gateway
- Auth Service
- File Service
- Sync Engine
- Metadata Service

## 数据流
Client -> API -> Auth -> File Service -> Storage

## 关键设计
- 元数据与文件解耦
- chunk-based upload
- event-driven sync（可扩展）

