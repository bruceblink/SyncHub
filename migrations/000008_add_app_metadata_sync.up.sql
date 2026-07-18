create table subscriptions (
    user_id uuid primary key references users(id) on delete cascade,
    plan text not null default 'free' check (plan in ('free', 'pro')),
    status text not null default 'active' check (status in ('active', 'past_due', 'canceled', 'expired')),
    expires_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

insert into subscriptions (user_id)
select id from users
on conflict (user_id) do nothing;

create table api_keys (
    id uuid primary key,
    user_id uuid not null references users(id) on delete cascade,
    name text not null,
    application text not null check (application in ('kvideo', 'latestnews')),
    key_prefix text not null,
    secret_hash text not null unique,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz not null default now()
);

create index api_keys_user_application_idx on api_keys(user_id, application, created_at desc);
create index api_keys_active_secret_idx on api_keys(secret_hash) where revoked_at is null;

create table app_metadata_documents (
    user_id uuid not null references users(id) on delete cascade,
    application text not null check (application in ('kvideo', 'latestnews')),
    collection text not null check (collection in ('watch-history', 'favorites', 'reading-history')),
    payload jsonb not null,
    version bigint not null default 1,
    updated_at timestamptz not null default now(),
    primary key (user_id, application, collection)
);
