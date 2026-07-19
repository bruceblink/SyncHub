alter table users alter column password_hash drop not null;

create table oauth_identities (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    provider text not null check (provider in ('github')),
    provider_user_id text not null,
    provider_login text not null default '',
    email citext not null,
    avatar_url text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (provider, provider_user_id),
    unique (user_id, provider)
);

create table oauth_login_codes (
    code_hash text primary key,
    user_id uuid not null references users(id) on delete cascade,
    expires_at timestamptz not null,
    consumed_at timestamptz,
    created_at timestamptz not null default now()
);

create index oauth_login_codes_expires_idx on oauth_login_codes(expires_at) where consumed_at is null;
