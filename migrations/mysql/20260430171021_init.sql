-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS stripeflow_products (
    id                VARCHAR(255) PRIMARY KEY,
    name              VARCHAR(255) NOT NULL,
    description       TEXT,
    active            BOOLEAN      NOT NULL DEFAULT TRUE,
    metadata          JSON         NOT NULL DEFAULT ('{}'),
    features          JSON         NOT NULL DEFAULT ('[]'),
    stripe_created_at DATETIME,
    created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stripeflow_prices (
    id                 VARCHAR(255) PRIMARY KEY,
    product_id         VARCHAR(255) NOT NULL,
    currency           VARCHAR(10)  NOT NULL,
    unit_amount        BIGINT,
    recurring_interval VARCHAR(20),
    recurring_count    INT,
    usage_type         VARCHAR(20)  NOT NULL DEFAULT '',
    type               VARCHAR(20)  NOT NULL DEFAULT '',
    nickname           TEXT         NOT NULL DEFAULT '',
    lookup_key         VARCHAR(255) NOT NULL DEFAULT '',
    active             BOOLEAN      NOT NULL DEFAULT TRUE,
    metadata           JSON         NOT NULL DEFAULT ('{}'),
    stripe_created_at  DATETIME,
    created_at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (product_id) REFERENCES stripeflow_products(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS stripeflow_subscriptions (
    id                      VARCHAR(36)  PRIMARY KEY,
    user_id                 VARCHAR(255) NOT NULL UNIQUE,
    stripe_customer_id      VARCHAR(255) UNIQUE,
    stripe_subscription_id  VARCHAR(255) UNIQUE,
    stripe_price_id         VARCHAR(255),
    stripe_product_id       VARCHAR(255),
    status                  VARCHAR(50)  NOT NULL DEFAULT 'none',
    trial_ends_at           DATETIME,
    current_period_start    DATETIME,
    current_period_end      DATETIME,
    canceled_at             DATETIME,
    usage_count             BIGINT       NOT NULL DEFAULT 0,
    usage_limit             BIGINT,
    metadata                JSON         NOT NULL DEFAULT ('{}'),
    created_at              DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at              DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE INDEX stripeflow_subs_customer ON stripeflow_subscriptions(stripe_customer_id);
CREATE INDEX stripeflow_subs_sub_id   ON stripeflow_subscriptions(stripe_subscription_id);

CREATE TABLE IF NOT EXISTS stripeflow_webhook_events (
    id          VARCHAR(255) PRIMARY KEY,
    type        VARCHAR(100) NOT NULL,
    processed   BOOLEAN      NOT NULL DEFAULT FALSE,
    payload     JSON         NOT NULL DEFAULT ('{}'),
    error       TEXT,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS stripeflow_webhook_events;
DROP TABLE IF EXISTS stripeflow_subscriptions;
DROP TABLE IF EXISTS stripeflow_prices;
DROP TABLE IF EXISTS stripeflow_products;
-- +goose StatementEnd
