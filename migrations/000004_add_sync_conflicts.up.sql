create table sync_conflicts (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    file_id uuid references file_nodes(id) on delete set null,
    path text not null,
    local_version bigint,
    remote_version bigint,
    resolution text not null default 'pending' check (resolution in ('pending', 'keep_local', 'keep_remote', 'keep_both')),
    created_at timestamptz not null default now(),
    resolved_at timestamptz
);

create index sync_conflicts_user_resolution_idx on sync_conflicts(user_id, resolution, created_at);
create index sync_conflicts_user_path_idx on sync_conflicts(user_id, path, created_at);
