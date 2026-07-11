drop trigger if exists file_versions_enqueue_object_gc on file_versions;
drop function if exists enqueue_deleted_file_version_object();
drop table if exists object_gc_queue;
