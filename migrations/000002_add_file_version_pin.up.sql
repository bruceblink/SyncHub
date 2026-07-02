alter table file_versions
    add column if not exists pinned_at timestamptz;
