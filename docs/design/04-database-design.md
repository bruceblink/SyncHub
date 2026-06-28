# 数据库设计

## 表结构

### user
- id
- email
- password_hash

### file_meta
- id
- user_id
- path
- hash
- version

### file_chunk
- id
- file_id
- chunk_index
- checksum

