alter table subscriptions
    add column currency text not null default 'USD',
    add column unit_amount bigint not null default 0 check (unit_amount >= 0),
    add column billing_interval text not null default 'month' check (billing_interval in ('month', 'year')),
    add column current_period_end timestamptz,
    add column cancel_at_period_end boolean not null default false,
    add column provider text,
    add column provider_customer_id text,
    add column provider_subscription_id text;

create unique index subscriptions_provider_customer_idx
    on subscriptions(provider, provider_customer_id)
    where provider is not null and provider_customer_id is not null;

create unique index subscriptions_provider_subscription_idx
    on subscriptions(provider, provider_subscription_id)
    where provider is not null and provider_subscription_id is not null;
