create extension if not exists citext;

create table users (
    id uuid primary key,
    email citext not null unique,
    password_hash text not null,
    status text not null default 'active',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table refresh_tokens (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    token_hash text not null unique,
    expires_at timestamptz not null,
    revoked_at timestamptz,
    created_at timestamptz not null default now()
);

create table file_nodes (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    parent_id uuid references file_nodes(id) on delete restrict,
    name text not null,
    path text not null,
    node_type text not null check (node_type in ('file', 'directory')),
    current_version_id uuid,
    size bigint not null default 0,
    sha256 text,
    storage_key text,
    version bigint not null default 1,
    deleted_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index file_nodes_user_path_active_idx on file_nodes(user_id, path) where deleted_at is null;
create unique index file_nodes_user_parent_name_active_idx on file_nodes(user_id, parent_id, name) where deleted_at is null;
create index file_nodes_user_parent_deleted_idx on file_nodes(user_id, parent_id, deleted_at);

create table file_versions (
    id uuid primary key,
    file_id uuid not null references file_nodes(id) on delete cascade,
    user_id uuid not null references users(id) on delete cascade,
    version bigint not null,
    size bigint not null,
    sha256 text not null,
    storage_key text not null,
    created_by_device_id uuid,
    created_at timestamptz not null default now(),
    unique (file_id, version)
);

alter table file_nodes
    add constraint file_nodes_current_version_fk
    foreign key (current_version_id) references file_versions(id) deferrable initially deferred;

create index file_versions_file_version_idx on file_versions(file_id, version desc);
create index file_versions_user_hash_size_idx on file_versions(user_id, sha256, size);

create table upload_sessions (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    target_path text not null,
    target_file_id uuid references file_nodes(id) on delete set null,
    base_version bigint,
    total_size bigint not null,
    chunk_size int not null,
    sha256 text not null,
    status text not null check (status in ('pending', 'committed', 'expired', 'aborted')),
    staging_key text not null,
    expires_at timestamptz not null,
    idempotency_key text,
    source_device_id uuid,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index upload_sessions_user_idempotency_idx on upload_sessions(user_id, idempotency_key) where idempotency_key is not null;
create index upload_sessions_user_status_expires_idx on upload_sessions(user_id, status, expires_at);

create table upload_chunks (
    id uuid primary key,
    upload_id uuid not null references upload_sessions(id) on delete cascade,
    chunk_index int not null,
    size int not null,
    sha256 text not null,
    storage_key text not null,
    created_at timestamptz not null default now(),
    unique (upload_id, chunk_index)
);

create table change_events (
    id bigserial primary key,
    user_id uuid not null references users(id) on delete cascade,
    file_id uuid not null,
    event_type text not null check (event_type in ('create', 'update', 'move', 'delete', 'restore')),
    version bigint,
    path text not null,
    old_path text,
    source_device_id uuid,
    created_at timestamptz not null default now()
);

create index change_events_user_id_idx on change_events(user_id, id);
create index change_events_user_file_created_idx on change_events(user_id, file_id, created_at);

create table devices (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    name text not null,
    platform text not null,
    last_seen_at timestamptz,
    last_applied_change_id bigint not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index devices_user_id_idx on devices(user_id);
