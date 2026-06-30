pragma foreign_keys = on;

create table if not exists users (
    id text primary key,
    email text not null collate nocase unique,
    password_hash text not null,
    status text not null default 'active',
    created_at datetime not null default current_timestamp,
    updated_at datetime not null default current_timestamp
);

create table if not exists refresh_tokens (
    id text primary key,
    user_id text not null references users(id) on delete cascade,
    token_hash text not null unique,
    expires_at datetime not null,
    revoked_at datetime,
    created_at datetime not null default current_timestamp
);

create table if not exists file_nodes (
    id text primary key,
    user_id text not null references users(id) on delete cascade,
    parent_id text references file_nodes(id) on delete restrict,
    name text not null,
    path text not null,
    node_type text not null check (node_type in ('file', 'directory')),
    current_version_id text references file_versions(id) deferrable initially deferred,
    size integer not null default 0,
    sha256 text,
    storage_key text,
    version integer not null default 1,
    deleted_at datetime,
    created_at datetime not null default current_timestamp,
    updated_at datetime not null default current_timestamp
);

create unique index if not exists file_nodes_user_path_active_idx on file_nodes(user_id, path) where deleted_at is null;
create unique index if not exists file_nodes_user_parent_name_active_idx on file_nodes(user_id, parent_id, name) where deleted_at is null;
create index if not exists file_nodes_user_parent_deleted_idx on file_nodes(user_id, parent_id, deleted_at);

create table if not exists file_versions (
    id text primary key,
    file_id text not null references file_nodes(id) on delete cascade,
    user_id text not null references users(id) on delete cascade,
    version integer not null,
    size integer not null,
    sha256 text not null,
    storage_key text not null,
    created_by_device_id text,
    created_at datetime not null default current_timestamp,
    unique (file_id, version)
);

create index if not exists file_versions_file_version_idx on file_versions(file_id, version desc);
create index if not exists file_versions_user_hash_size_idx on file_versions(user_id, sha256, size);

create table if not exists upload_sessions (
    id text primary key,
    user_id text not null references users(id) on delete cascade,
    target_path text not null,
    target_file_id text references file_nodes(id) on delete set null,
    base_version integer,
    total_size integer not null,
    chunk_size integer not null,
    sha256 text not null,
    status text not null check (status in ('pending', 'committed', 'expired', 'aborted')),
    staging_key text not null,
    expires_at datetime not null,
    idempotency_key text,
    created_at datetime not null default current_timestamp,
    updated_at datetime not null default current_timestamp
);

create unique index if not exists upload_sessions_user_idempotency_idx on upload_sessions(user_id, idempotency_key) where idempotency_key is not null;
create index if not exists upload_sessions_user_status_expires_idx on upload_sessions(user_id, status, expires_at);

create table if not exists upload_chunks (
    id text primary key,
    upload_id text not null references upload_sessions(id) on delete cascade,
    chunk_index integer not null,
    size integer not null,
    sha256 text not null,
    storage_key text not null,
    created_at datetime not null default current_timestamp,
    unique (upload_id, chunk_index)
);

create table if not exists change_events (
    id integer primary key autoincrement,
    user_id text not null references users(id) on delete cascade,
    file_id text not null,
    event_type text not null check (event_type in ('create', 'update', 'move', 'delete', 'restore')),
    version integer,
    path text not null,
    old_path text,
    source_device_id text,
    created_at datetime not null default current_timestamp
);

create index if not exists change_events_user_id_idx on change_events(user_id, id);
create index if not exists change_events_user_file_created_idx on change_events(user_id, file_id, created_at);

create table if not exists devices (
    id text primary key,
    user_id text not null references users(id) on delete cascade,
    name text not null,
    platform text not null,
    last_seen_at datetime,
    last_applied_change_id integer not null default 0,
    created_at datetime not null default current_timestamp,
    updated_at datetime not null default current_timestamp
);

create index if not exists devices_user_id_idx on devices(user_id);

create table if not exists sync_conflicts (
    id text primary key,
    user_id text not null references users(id) on delete cascade,
    file_id text references file_nodes(id) on delete set null,
    path text not null,
    local_version integer,
    remote_version integer,
    resolution text not null default 'pending' check (resolution in ('pending', 'keep_local', 'keep_remote', 'keep_both')),
    created_at datetime not null default current_timestamp,
    resolved_at datetime
);

create index if not exists sync_conflicts_user_resolution_idx on sync_conflicts(user_id, resolution, created_at);
create index if not exists sync_conflicts_user_path_idx on sync_conflicts(user_id, path, created_at);
