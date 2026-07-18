drop index if exists subscriptions_provider_subscription_idx;
drop index if exists subscriptions_provider_customer_idx;

alter table subscriptions
    drop column if exists provider_subscription_id,
    drop column if exists provider_customer_id,
    drop column if exists provider,
    drop column if exists cancel_at_period_end,
    drop column if exists current_period_end,
    drop column if exists billing_interval,
    drop column if exists unit_amount,
    drop column if exists currency;
