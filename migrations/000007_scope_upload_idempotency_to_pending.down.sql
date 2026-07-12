drop index if exists upload_sessions_user_idempotency_pending_idx;

create unique index upload_sessions_user_idempotency_idx
on upload_sessions(user_id, idempotency_key)
where idempotency_key is not null;
