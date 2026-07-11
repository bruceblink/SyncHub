alter table devices drop constraint if exists devices_last_sync_status_check;
alter table devices drop column if exists last_sync_error;
alter table devices drop column if exists last_sync_status;
alter table devices drop column if exists last_sync_at;
