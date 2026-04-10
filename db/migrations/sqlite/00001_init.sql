-- +goose Up
-- +goose StatementBegin
CREATE TABLE stripeflow_plans (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    stripe_product_id TEXT NOT NULL,
    stripe_price_id TEXT NOT NULL UNIQUE,
    description TEXT,
    price_usd INTEGER NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 0,
    billing_cycle TEXT NOT NULL,
    features TEXT,
    sort_order INTEGER DEFAULT 0,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE stripeflow_subscriptions (
    id TEXT PRIMARY KEY,
    stripe_customer_id TEXT NOT NULL,
    stripe_subscription_id TEXT NOT NULL,
    stripe_price_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    plan_name TEXT,
    status TEXT,
    metadata TEXT,
    date_start DATETIME,
    date_end DATETIME,
    date_renewal DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(stripe_customer_id, stripe_subscription_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stripeflow_subscriptions;
DROP TABLE IF EXISTS stripeflow_plans;
-- +goose StatementEnd
