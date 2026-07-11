alter table devices add column last_sync_at timestamptz;
alter table devices add column last_sync_status text;
alter table devices add column last_sync_error text;

alter table devices add constraint devices_last_sync_status_check
    check (last_sync_status is null or last_sync_status in ('success', 'error'));
