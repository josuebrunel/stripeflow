-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE stripeflow_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    stripe_product_id VARCHAR(255) NOT NULL,
    stripe_price_id VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    price_usd INTEGER NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    billing_cycle VARCHAR(50) NOT NULL,
    features JSONB,
    sort_order INTEGER DEFAULT 0,
    max_descriptions INTEGER DEFAULT 0,
    max_photos INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE stripeflow_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stripe_customer_id VARCHAR(255) NOT NULL,
    stripe_subscription_id VARCHAR(255) NOT NULL,
    stripe_price_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    plan_name VARCHAR(255),
    status VARCHAR(50),
    usage_desc INTEGER DEFAULT 0,
    usage_photos INTEGER DEFAULT 0,
    date_start TIMESTAMP WITH TIME ZONE,
    date_end TIMESTAMP WITH TIME ZONE,
    date_renewal TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(stripe_customer_id, stripe_subscription_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stripeflow_subscriptions;
DROP TABLE IF EXISTS stripeflow_plans;
DROP EXTENSION IF EXISTS pgcrypto;
-- +goose StatementEnd
