-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Products synced from Stripe
CREATE TABLE stripeflow_products (
    id                TEXT        PRIMARY KEY,  -- Stripe product ID (prod_...)
    name              TEXT        NOT NULL,
    description       TEXT,
    active            BOOLEAN     NOT NULL DEFAULT TRUE,
    metadata          JSONB       NOT NULL DEFAULT '{}',
    features          JSONB       NOT NULL DEFAULT '[]',
    stripe_created_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Prices (each product can have many prices)
CREATE TABLE stripeflow_prices (
    id                 TEXT    PRIMARY KEY,  -- Stripe price ID (price_...)
    product_id         TEXT    NOT NULL REFERENCES stripeflow_products(id) ON DELETE CASCADE,
    currency           TEXT    NOT NULL,
    unit_amount        BIGINT,              -- smallest currency unit (e.g. cents)
    recurring_interval TEXT,               -- month | year | week | day | NULL for one-time
    recurring_count    INTEGER,
    usage_type         TEXT    NOT NULL DEFAULT '',  -- licensed | metered | empty for one-time
    type               TEXT    NOT NULL DEFAULT '',  -- recurring | one_time
    nickname           TEXT    NOT NULL DEFAULT '',  -- human-readable label from Stripe
    lookup_key         TEXT    NOT NULL DEFAULT '',  -- stable key for referencing without ID
    active             BOOLEAN NOT NULL DEFAULT TRUE,
    metadata           JSONB   NOT NULL DEFAULT '{}',
    stripe_created_at  TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per user – tracks Stripe customer + subscription state
CREATE TABLE stripeflow_subscriptions (
    id                      BIGSERIAL   PRIMARY KEY,
    user_id                 TEXT        NOT NULL UNIQUE,
    stripe_customer_id      TEXT        UNIQUE,
    stripe_subscription_id  TEXT        UNIQUE,
    stripe_price_id         TEXT,
    stripe_product_id       TEXT,
    status                  TEXT        NOT NULL DEFAULT 'none',
    trial_ends_at           TIMESTAMPTZ,
    current_period_start    TIMESTAMPTZ,
    current_period_end      TIMESTAMPTZ,
    canceled_at             TIMESTAMPTZ,
    usage_count             BIGINT      NOT NULL DEFAULT 0,
    usage_limit             BIGINT,      -- NULL = unlimited
    metadata                JSONB       NOT NULL DEFAULT '{}',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only log of processed Stripe webhook events (idempotency)
CREATE TABLE stripeflow_webhook_events (
    id          TEXT    PRIMARY KEY,  -- Stripe event ID (evt_...)
    type        TEXT    NOT NULL,
    processed   BOOLEAN NOT NULL DEFAULT FALSE,
    payload     JSONB   NOT NULL DEFAULT '{}',
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX stripeflow_subs_customer ON stripeflow_subscriptions(stripe_customer_id);
CREATE INDEX stripeflow_subs_sub_id   ON stripeflow_subscriptions(stripe_subscription_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stripeflow_webhook_events;
DROP TABLE IF EXISTS stripeflow_subscriptions;
DROP TABLE IF EXISTS stripeflow_prices;
DROP TABLE IF EXISTS stripeflow_products;
DROP EXTENSION IF EXISTS pgcrypto;
-- +goose StatementEnd
