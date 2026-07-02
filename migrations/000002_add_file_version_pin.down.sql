alter table file_versions
    drop column if exists pinned_at;
