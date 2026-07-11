create table object_gc_queue (
    storage_key text primary key,
    status text not null default 'pending' check (status in ('pending', 'processing')),
    available_at timestamptz not null,
    attempts int not null default 0,
    last_error text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index object_gc_queue_available_idx on object_gc_queue(status, available_at, storage_key);

create function enqueue_deleted_file_version_object() returns trigger as $$
begin
    insert into object_gc_queue (storage_key, available_at)
    values (old.storage_key, now() + interval '1 hour')
    on conflict (storage_key) do update set
        status = 'pending',
        available_at = greatest(object_gc_queue.available_at, excluded.available_at),
        updated_at = now();
    return old;
end;
$$ language plpgsql;

create trigger file_versions_enqueue_object_gc
after delete on file_versions
for each row execute function enqueue_deleted_file_version_object();
