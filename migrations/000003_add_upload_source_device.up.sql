alter table upload_sessions
    add column if not exists source_device_id uuid;
