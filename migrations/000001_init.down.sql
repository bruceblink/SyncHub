drop table if exists devices;
drop table if exists change_events;
drop table if exists upload_chunks;
drop table if exists upload_sessions;
alter table if exists file_nodes drop constraint if exists file_nodes_current_version_fk;
drop table if exists file_versions;
drop table if exists file_nodes;
drop table if exists refresh_tokens;
drop table if exists users;
