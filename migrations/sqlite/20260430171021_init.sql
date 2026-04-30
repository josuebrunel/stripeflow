-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stripeflow_products (
    id                TEXT    PRIMARY KEY,
    name              TEXT    NOT NULL,
    description       TEXT,
    active            INTEGER NOT NULL DEFAULT 1,
    metadata          TEXT    NOT NULL DEFAULT '{}',
    features          TEXT    NOT NULL DEFAULT '[]',
    stripe_created_at DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stripeflow_prices (
    id                 TEXT    PRIMARY KEY,
    product_id         TEXT    NOT NULL REFERENCES stripeflow_products(id) ON DELETE CASCADE,
    currency           TEXT    NOT NULL,
    unit_amount        INTEGER,
    recurring_interval TEXT,
    recurring_count    INTEGER,
    usage_type         TEXT    NOT NULL DEFAULT '',
    type               TEXT    NOT NULL DEFAULT '',
    nickname           TEXT    NOT NULL DEFAULT '',
    lookup_key         TEXT    NOT NULL DEFAULT '',
    active             INTEGER NOT NULL DEFAULT 1,
    metadata           TEXT    NOT NULL DEFAULT '{}',
    stripe_created_at  DATETIME,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stripeflow_subscriptions (
    id                      INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id                 TEXT     NOT NULL UNIQUE,
    stripe_customer_id      TEXT     UNIQUE,
    stripe_subscription_id  TEXT     UNIQUE,
    stripe_price_id         TEXT,
    stripe_product_id       TEXT,
    status                  TEXT     NOT NULL DEFAULT 'none',
    trial_ends_at           DATETIME,
    current_period_start    DATETIME,
    current_period_end      DATETIME,
    canceled_at             DATETIME,
    usage_count             INTEGER  NOT NULL DEFAULT 0,
    usage_limit             INTEGER,
    metadata                TEXT     NOT NULL DEFAULT '{}',
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS stripeflow_subs_customer ON stripeflow_subscriptions(stripe_customer_id);
CREATE INDEX IF NOT EXISTS stripeflow_subs_sub_id   ON stripeflow_subscriptions(stripe_subscription_id);

CREATE TABLE IF NOT EXISTS stripeflow_webhook_events (
    id          TEXT    PRIMARY KEY,
    type        TEXT    NOT NULL,
    processed   INTEGER NOT NULL DEFAULT 0,
    payload     TEXT    NOT NULL DEFAULT '{}',
    error       TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stripeflow_webhook_events;
DROP TABLE IF EXISTS stripeflow_subscriptions;
DROP TABLE IF EXISTS stripeflow_prices;
DROP TABLE IF EXISTS stripeflow_products;
-- +goose StatementEnd
