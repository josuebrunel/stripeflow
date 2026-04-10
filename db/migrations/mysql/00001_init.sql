-- +goose Up
-- +goose StatementBegin
CREATE TABLE stripeflow_plans (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL,
    stripe_product_id VARCHAR(255) NOT NULL,
    stripe_price_id VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    price_usd INT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    billing_cycle VARCHAR(50) NOT NULL,
    features JSON,
    sort_order INT DEFAULT 0,
    max_descriptions INT DEFAULT 0,
    max_photos INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE stripeflow_subscriptions (
    id VARCHAR(36) PRIMARY KEY,
    stripe_customer_id VARCHAR(255) NOT NULL,
    stripe_subscription_id VARCHAR(255) NOT NULL,
    stripe_price_id VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    plan_name VARCHAR(255),
    status VARCHAR(50),
    usage_desc INT DEFAULT 0,
    usage_photos INT DEFAULT 0,
    date_start TIMESTAMP,
    date_end TIMESTAMP,
    date_renewal TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE(stripe_customer_id, stripe_subscription_id)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stripeflow_subscriptions;
DROP TABLE IF EXISTS stripeflow_plans;
-- +goose StatementEnd
