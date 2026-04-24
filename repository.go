package stripeflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// --------------------------------------------------------------------------
// SQL query sets per dialect
// --------------------------------------------------------------------------

type queries struct {
	// Subscriptions
	upsertSub         string
	createEmptySub    string
	findSubByUser     string
	findSubByCustomer string
	findSubByStripeID string
	findSubByID       string
	incrementUsage    string
	setUsageLimit     string
	resetUsage        string
	deleteSub         string

	// Products
	upsertProduct          string
	listProducts           string
	getProductByID         string
	deleteProduct          string
	deleteAllProducts      string
	deletePricesForProduct string
	deleteAllPrices        string

	// Prices
	upsertPrice         string
	listPricesByProduct string

	// Webhook idempotency
	markEventProcessing string
	markEventDone       string

	// Dialect hint
	isPostgres bool
	isMySQL    bool
}

func newQueries(dialect string) (queries, error) {
	switch dialect {
	case "postgres":
		return pgQueries, nil
	case "mysql":
		return myQueries, nil
	case "sqlite", "sqlite3":
		return slQueries, nil
	default:
		return queries{}, fmt.Errorf("stripeflow: unsupported dialect %q", dialect)
	}
}

// --------------------------------------------------------------------------
// Postgres
// --------------------------------------------------------------------------

var pgQueries = queries{
	isPostgres: true,

	upsertSub: `
		INSERT INTO stripeflow_subscriptions
		    (user_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, stripe_product_id,
		     status, trial_ends_at, current_period_start, current_period_end, canceled_at, metadata, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,COALESCE($11, '{}')::jsonb,NOW())
		ON CONFLICT (user_id) DO UPDATE SET
		    stripe_customer_id      = EXCLUDED.stripe_customer_id,
		    stripe_subscription_id  = EXCLUDED.stripe_subscription_id,
		    stripe_price_id         = EXCLUDED.stripe_price_id,
		    stripe_product_id       = EXCLUDED.stripe_product_id,
		    status                  = EXCLUDED.status,
		    trial_ends_at           = EXCLUDED.trial_ends_at,
		    current_period_start    = EXCLUDED.current_period_start,
		    current_period_end      = EXCLUDED.current_period_end,
		    canceled_at             = EXCLUDED.canceled_at,
		    metadata                = EXCLUDED.metadata,
		    updated_at              = NOW()`,

	createEmptySub: `
		INSERT INTO stripeflow_subscriptions (user_id, status)
		VALUES ($1, 'none')
		ON CONFLICT (user_id) DO NOTHING`,

	findSubByID: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE id = $1`,

	findSubByUser: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE user_id = $1`,

	findSubByCustomer: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_customer_id = $1`,

	findSubByStripeID: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_subscription_id = $1`,

	incrementUsage: `
		UPDATE stripeflow_subscriptions
		SET usage_count = usage_count + $2, updated_at = NOW()
		WHERE user_id = $1
		RETURNING usage_count`,

	setUsageLimit: `
		UPDATE stripeflow_subscriptions SET usage_limit = $2, updated_at = NOW() WHERE user_id = $1`,

	resetUsage: `
		UPDATE stripeflow_subscriptions SET usage_count = 0, updated_at = NOW() WHERE user_id = $1`,

	deleteSub: `DELETE FROM stripeflow_subscriptions WHERE user_id = $1`,

	upsertProduct: `
		INSERT INTO stripeflow_products (id, name, description, active, metadata, features, stripe_created_at, updated_at)
		VALUES ($1,$2,$3,$4,COALESCE($5, '{}')::jsonb,COALESCE($6, '[]')::jsonb,$7,NOW())
		ON CONFLICT (id) DO UPDATE SET
		    name              = EXCLUDED.name,
		    description       = EXCLUDED.description,
		    active            = EXCLUDED.active,
		    metadata          = EXCLUDED.metadata,
		    features          = EXCLUDED.features,
		    stripe_created_at = EXCLUDED.stripe_created_at,
		    updated_at        = NOW()`,

	deleteProduct:          `DELETE FROM stripeflow_products WHERE id = $1`,
	deleteAllProducts:      `DELETE FROM stripeflow_products`,
	deletePricesForProduct: `DELETE FROM stripeflow_prices WHERE product_id = $1`,
	deleteAllPrices:        `DELETE FROM stripeflow_prices`,
	getProductByID: `
		SELECT id, name, COALESCE(description,''), active, metadata, features, stripe_created_at, created_at, updated_at
		FROM stripeflow_products WHERE id = $1`,

	listProducts: `
		SELECT id, name, COALESCE(description,''), active, metadata, features, stripe_created_at, created_at, updated_at
		FROM stripeflow_products`,

	upsertPrice: `
		INSERT INTO stripeflow_prices
		    (id, product_id, currency, unit_amount, recurring_interval, recurring_count, active, metadata, stripe_created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,COALESCE($8, '{}')::jsonb,$9,NOW())
		ON CONFLICT (id) DO UPDATE SET
		    product_id         = EXCLUDED.product_id,
		    currency           = EXCLUDED.currency,
		    unit_amount        = EXCLUDED.unit_amount,
		    recurring_interval = EXCLUDED.recurring_interval,
		    recurring_count    = EXCLUDED.recurring_count,
		    active             = EXCLUDED.active,
		    metadata           = EXCLUDED.metadata,
		    stripe_created_at  = EXCLUDED.stripe_created_at,
		    updated_at         = NOW()`,

	listPricesByProduct: `
		SELECT id, product_id, currency, unit_amount, COALESCE(recurring_interval,''), recurring_count,
		       active, metadata, stripe_created_at, created_at, updated_at
		FROM stripeflow_prices WHERE product_id = $1 ORDER BY unit_amount ASC`,

	markEventProcessing: `
		INSERT INTO stripeflow_webhook_events (id, type) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING`,

	markEventDone: `
		UPDATE stripeflow_webhook_events
		SET processed = TRUE, error = NULLIF($2,'')
		WHERE id = $1`,
}

// --------------------------------------------------------------------------
// MySQL
// --------------------------------------------------------------------------

var myQueries = queries{
	isMySQL: true,

	upsertSub: `
		INSERT INTO stripeflow_subscriptions
		    (id, user_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, stripe_product_id,
		     status, trial_ends_at, current_period_start, current_period_end, canceled_at, metadata)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,COALESCE(?, '{}'))
		ON DUPLICATE KEY UPDATE
		    stripe_customer_id      = VALUES(stripe_customer_id),
		    stripe_subscription_id  = VALUES(stripe_subscription_id),
		    stripe_price_id         = VALUES(stripe_price_id),
		    stripe_product_id       = VALUES(stripe_product_id),
		    status                  = VALUES(status),
		    trial_ends_at           = VALUES(trial_ends_at),
		    current_period_start    = VALUES(current_period_start),
		    current_period_end      = VALUES(current_period_end),
		    canceled_at             = VALUES(canceled_at),
		    metadata                = VALUES(metadata),
		    updated_at              = CURRENT_TIMESTAMP`,

	createEmptySub: `
		INSERT IGNORE INTO stripeflow_subscriptions (id, user_id, status) VALUES (?,?,'none')`,

	findSubByID: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE id = ?`,

	findSubByUser: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE user_id = ?`,

	findSubByCustomer: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_customer_id = ?`,

	findSubByStripeID: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_subscription_id = ?`,

	incrementUsage: `
		UPDATE stripeflow_subscriptions SET usage_count = usage_count + ?, updated_at = NOW() WHERE user_id = ?`,

	setUsageLimit: `
		UPDATE stripeflow_subscriptions SET usage_limit = ?, updated_at = NOW() WHERE user_id = ?`,

	resetUsage: `
		UPDATE stripeflow_subscriptions SET usage_count = 0, updated_at = NOW() WHERE user_id = ?`,

	deleteSub: `DELETE FROM stripeflow_subscriptions WHERE user_id = ?`,

	upsertProduct: `
		INSERT INTO stripeflow_products (id, name, description, active, metadata, features, stripe_created_at, updated_at)
		VALUES (?,?,?,?,COALESCE(?, '{}'),COALESCE(?, '[]'),?,NOW())
		ON DUPLICATE KEY UPDATE
		    name = VALUES(name), description = VALUES(description),
		    active = VALUES(active), metadata = VALUES(metadata), features = VALUES(features),
		    stripe_created_at = VALUES(stripe_created_at), updated_at = NOW()`,

	deleteProduct:          `DELETE FROM stripeflow_products WHERE id = ?`,
	deleteAllProducts:      `DELETE FROM stripeflow_products`,
	deletePricesForProduct: `DELETE FROM stripeflow_prices WHERE product_id = ?`,
	deleteAllPrices:        `DELETE FROM stripeflow_prices`,
	getProductByID: `
		SELECT id, name, COALESCE(description,''), active, metadata, features, stripe_created_at, created_at, updated_at
		FROM stripeflow_products WHERE id = ?`,

	listProducts: `
		SELECT id, name, COALESCE(description,''), active, metadata, features, stripe_created_at, created_at, updated_at
		FROM stripeflow_products`,

	upsertPrice: `
		INSERT INTO stripeflow_prices
		    (id, product_id, currency, unit_amount, recurring_interval, recurring_count, active, metadata, stripe_created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,COALESCE(?, '{}'),?,NOW())
		ON DUPLICATE KEY UPDATE
		    product_id = VALUES(product_id), currency = VALUES(currency),
		    unit_amount = VALUES(unit_amount), recurring_interval = VALUES(recurring_interval),
		    recurring_count = VALUES(recurring_count), active = VALUES(active),
		    metadata = VALUES(metadata), stripe_created_at = VALUES(stripe_created_at), updated_at = NOW()`,

	listPricesByProduct: `
		SELECT id, product_id, currency, unit_amount, COALESCE(recurring_interval,''), recurring_count,
		       active, metadata, stripe_created_at, created_at, updated_at
		FROM stripeflow_prices WHERE product_id = ? ORDER BY unit_amount ASC`,

	markEventProcessing: `
		INSERT IGNORE INTO stripeflow_webhook_events (id, type) VALUES (?,?)`,

	markEventDone: `
		UPDATE stripeflow_webhook_events SET processed = TRUE, error = NULLIF(?,''  ) WHERE id = ?`,
}

// --------------------------------------------------------------------------
// SQLite
// --------------------------------------------------------------------------

var slQueries = queries{
	upsertSub: `
		INSERT INTO stripeflow_subscriptions
		    (user_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, stripe_product_id,
		     status, trial_ends_at, current_period_start, current_period_end, canceled_at, metadata, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,COALESCE(?, '{}'),CURRENT_TIMESTAMP)
		ON CONFLICT (user_id) DO UPDATE SET
		    stripe_customer_id      = EXCLUDED.stripe_customer_id,
		    stripe_subscription_id  = EXCLUDED.stripe_subscription_id,
		    stripe_price_id         = EXCLUDED.stripe_price_id,
		    stripe_product_id       = EXCLUDED.stripe_product_id,
		    status                  = EXCLUDED.status,
		    trial_ends_at           = EXCLUDED.trial_ends_at,
		    current_period_start    = EXCLUDED.current_period_start,
		    current_period_end      = EXCLUDED.current_period_end,
		    canceled_at             = EXCLUDED.canceled_at,
		    metadata                = EXCLUDED.metadata,
		    updated_at              = CURRENT_TIMESTAMP`,

	createEmptySub: `
		INSERT OR IGNORE INTO stripeflow_subscriptions (user_id, status) VALUES (?,'none')`,

	findSubByID: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE id = ?`,

	findSubByUser: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE user_id = ?`,

	findSubByCustomer: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_customer_id = ?`,

	findSubByStripeID: `
		SELECT id, user_id,
		       COALESCE(stripe_customer_id,''), COALESCE(stripe_subscription_id,''),
		       COALESCE(stripe_price_id,''), COALESCE(stripe_product_id,''),
		       status, trial_ends_at, current_period_start, current_period_end,
		       canceled_at, usage_count, usage_limit, metadata, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_subscription_id = ?`,

	incrementUsage: `
		UPDATE stripeflow_subscriptions SET usage_count = usage_count + ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? RETURNING usage_count`,

	setUsageLimit: `
		UPDATE stripeflow_subscriptions SET usage_limit = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,

	resetUsage: `
		UPDATE stripeflow_subscriptions SET usage_count = 0, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,

	deleteSub: `DELETE FROM stripeflow_subscriptions WHERE user_id = ?`,

	upsertProduct: `
		INSERT INTO stripeflow_products (id, name, description, active, metadata, features, stripe_created_at, updated_at)
		VALUES (?,?,?,?,COALESCE(?, '{}'),COALESCE(?, '[]'),?,CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
		    name = EXCLUDED.name, description = EXCLUDED.description,
		    active = EXCLUDED.active, metadata = EXCLUDED.metadata, features = EXCLUDED.features,
		    stripe_created_at = EXCLUDED.stripe_created_at,
		    updated_at = CURRENT_TIMESTAMP`,

	deleteProduct:          `DELETE FROM stripeflow_products WHERE id = ?`,
	deleteAllProducts:      `DELETE FROM stripeflow_products`,
	deletePricesForProduct: `DELETE FROM stripeflow_prices WHERE product_id = ?`,
	deleteAllPrices:        `DELETE FROM stripeflow_prices`,
	getProductByID: `
		SELECT id, name, COALESCE(description,''), active, metadata, features, stripe_created_at, created_at, updated_at
		FROM stripeflow_products WHERE id = ?`,

	listProducts: `
		SELECT id, name, COALESCE(description,''), active, metadata, features, stripe_created_at, created_at, updated_at
		FROM stripeflow_products`,

	upsertPrice: `
		INSERT INTO stripeflow_prices
		    (id, product_id, currency, unit_amount, recurring_interval, recurring_count, active, metadata, stripe_created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,COALESCE(?, '{}'),?,CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
		    product_id = EXCLUDED.product_id, currency = EXCLUDED.currency,
		    unit_amount = EXCLUDED.unit_amount, recurring_interval = EXCLUDED.recurring_interval,
		    recurring_count = EXCLUDED.recurring_count, active = EXCLUDED.active,
		    metadata = EXCLUDED.metadata,
		    stripe_created_at = EXCLUDED.stripe_created_at, updated_at = CURRENT_TIMESTAMP`,

	listPricesByProduct: `
		SELECT id, product_id, currency, unit_amount, COALESCE(recurring_interval,''), recurring_count,
		       active, metadata, stripe_created_at, created_at, updated_at
		FROM stripeflow_prices WHERE product_id = ? ORDER BY unit_amount ASC`,

	markEventProcessing: `
		INSERT OR IGNORE INTO stripeflow_webhook_events (id, type) VALUES (?,?)`,

	markEventDone: `
		UPDATE stripeflow_webhook_events SET processed = 1, error = NULLIF(?,''  ) WHERE id = ?`,
}

// --------------------------------------------------------------------------
// Repository
// --------------------------------------------------------------------------

type repository struct {
	db      *sql.DB
	q       queries
	dialect string
}

func newRepository(db *sql.DB, dialect string) (*repository, error) {
	q, err := newQueries(dialect)
	if err != nil {
		return nil, err
	}
	return &repository{db: db, q: q, dialect: dialect}, nil
}

// --------------------------------------------------------------------------
// Subscription helpers
// --------------------------------------------------------------------------

type upsertSubParams struct {
	UserID               string
	StripeCustomerID     string
	StripeSubscriptionID string
	StripePriceID        string
	StripeProductID      string
	Status               SubscriptionStatus
	TrialEndsAt          *time.Time
	CurrentPeriodStart   *time.Time
	CurrentPeriodEnd     *time.Time
	CanceledAt           *time.Time
	Metadata             []byte
}

func (r *repository) upsertSubscription(ctx context.Context, p upsertSubParams) error {
	var err error
	var meta interface{} = p.Metadata
	if len(p.Metadata) == 0 {
		meta = nil
	}
	if r.q.isMySQL {
		id := uuid.NewString()
		_, err = r.db.ExecContext(ctx, r.q.upsertSub,
			id, p.UserID, nullStr(p.StripeCustomerID), nullStr(p.StripeSubscriptionID),
			nullStr(p.StripePriceID), nullStr(p.StripeProductID),
			string(p.Status),
			p.TrialEndsAt, p.CurrentPeriodStart, p.CurrentPeriodEnd, p.CanceledAt, meta,
		)
	} else {
		_, err = r.db.ExecContext(ctx, r.q.upsertSub,
			p.UserID, nullStr(p.StripeCustomerID), nullStr(p.StripeSubscriptionID),
			nullStr(p.StripePriceID), nullStr(p.StripeProductID),
			string(p.Status),
			p.TrialEndsAt, p.CurrentPeriodStart, p.CurrentPeriodEnd, p.CanceledAt, meta,
		)
	}
	return err
}

func (r *repository) createEmptySubscription(ctx context.Context, userID string) error {
	if r.q.isMySQL {
		_, err := r.db.ExecContext(ctx, r.q.createEmptySub, uuid.NewString(), userID)
		return err
	}
	_, err := r.db.ExecContext(ctx, r.q.createEmptySub, userID)
	return err
}

func (r *repository) scanSubscription(row *sql.Row) (*Subscription, error) {
	sub := &Subscription{}
	var metaBytes []byte
	err := row.Scan(
		&sub.ID, &sub.UserID,
		&sub.StripeCustomerID, &sub.StripeSubscriptionID,
		&sub.StripePriceID, &sub.StripeProductID,
		&sub.Status, &sub.TrialEndsAt, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
		&sub.CanceledAt, &sub.UsageCount, &sub.UsageLimit, &metaBytes,
		&sub.CreatedAt, &sub.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNoSubscription
	}
	if len(metaBytes) > 0 {
		m := json.RawMessage(metaBytes)
		sub.Metadata = &m
	}
	return sub, err
}

func (r *repository) getSubscriptionByUserID(ctx context.Context, userID string) (*Subscription, error) {
	return r.scanSubscription(r.db.QueryRowContext(ctx, r.q.findSubByUser, userID))
}

func (r *repository) getSubscriptionByCustomerID(ctx context.Context, customerID string) (*Subscription, error) {
	return r.scanSubscription(r.db.QueryRowContext(ctx, r.q.findSubByCustomer, customerID))
}

func (r *repository) getSubscriptionByStripeSubID(ctx context.Context, subID string) (*Subscription, error) {
	return r.scanSubscription(r.db.QueryRowContext(ctx, r.q.findSubByStripeID, subID))
}

func (r *repository) incrementUsage(ctx context.Context, userID string, delta int64) (int64, error) {
	var count int64
	if r.q.isMySQL {
		// MySQL: no RETURNING; args order: (delta, userID)
		if _, err := r.db.ExecContext(ctx, r.q.incrementUsage, delta, userID); err != nil {
			return 0, err
		}
		err := r.db.QueryRowContext(ctx,
			`SELECT usage_count FROM stripeflow_subscriptions WHERE user_id = ?`, userID,
		).Scan(&count)
		return count, err
	}
	if r.q.isPostgres {
		// Postgres: RETURNING; args order: (userID, delta)
		err := r.db.QueryRowContext(ctx, r.q.incrementUsage, userID, delta).Scan(&count)
		return count, err
	}
	// SQLite: RETURNING; args order: (delta, userID)
	err := r.db.QueryRowContext(ctx, r.q.incrementUsage, delta, userID).Scan(&count)
	return count, err
}

func (r *repository) setUsageLimit(ctx context.Context, userID string, limit *int64) error {
	if r.q.isMySQL {
		_, err := r.db.ExecContext(ctx, r.q.setUsageLimit, limit, userID)
		return err
	}
	_, err := r.db.ExecContext(ctx, r.q.setUsageLimit, userID, limit)
	return err
}

func (r *repository) resetUsage(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, r.q.resetUsage, userID)
	return err
}

func (r *repository) deleteSubscription(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, r.q.deleteSub, userID)
	return err
}

// --------------------------------------------------------------------------
// Product / Price helpers
// --------------------------------------------------------------------------

func (r *repository) upsertProduct(ctx context.Context, p Product) error {
	var meta, features interface{}
	if p.Metadata != nil {
		meta = []byte(*p.Metadata)
	}
	if p.Features != nil {
		features = []byte(*p.Features)
	}
	_, err := r.db.ExecContext(ctx, r.q.upsertProduct,
		p.ID, p.Name, p.Description, p.Active, meta, features, p.StripeCreatedAt,
	)
	return err
}

func (r *repository) listProducts(ctx context.Context, activeOnly bool) ([]Product, error) {
	q := r.q.listProducts
	var args []any
	if activeOnly {
		if r.q.isMySQL || !r.q.isPostgres {
			q += " WHERE active = 1"
		} else {
			q += " WHERE active = TRUE"
		}
	}
	q += " ORDER BY created_at DESC"

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		var metaBytes, featBytes []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Active,
			&metaBytes, &featBytes, &p.StripeCreatedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if len(metaBytes) > 0 {
			m := json.RawMessage(metaBytes)
			p.Metadata = &m
		}
		if len(featBytes) > 0 {
			f := json.RawMessage(featBytes)
			p.Features = &f
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func (r *repository) upsertPrice(ctx context.Context, p Price) error {
	var meta interface{}
	if p.Metadata != nil {
		meta = []byte(*p.Metadata)
	}
	_, err := r.db.ExecContext(ctx, r.q.upsertPrice,
		p.ID, p.ProductID, p.Currency, p.UnitAmount,
		nullStr(p.RecurringInterval), p.RecurringCount,
		p.Active, meta, p.StripeCreatedAt,
	)
	return err
}

func (r *repository) listPricesForProduct(ctx context.Context, productID string) ([]Price, error) {
	rows, err := r.db.QueryContext(ctx, r.q.listPricesByProduct, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prices []Price
	for rows.Next() {
		var p Price
		var metaBytes []byte
		if err := rows.Scan(
			&p.ID, &p.ProductID, &p.Currency, &p.UnitAmount, &p.RecurringInterval, &p.RecurringCount,
			&p.Active, &metaBytes, &p.StripeCreatedAt, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(metaBytes) > 0 {
			m := json.RawMessage(metaBytes)
			p.Metadata = &m
		}
		prices = append(prices, p)
	}
	return prices, rows.Err()
}

// --------------------------------------------------------------------------
// Webhook idempotency helpers
// --------------------------------------------------------------------------

// markEventProcessing inserts the event ID. Returns true if it was already present.
func (r *repository) markEventProcessing(ctx context.Context, eventID, eventType string) (alreadyProcessed bool, err error) {
	res, err := r.db.ExecContext(ctx, r.q.markEventProcessing, eventID, eventType)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 0, nil
}

func (r *repository) markEventDone(ctx context.Context, eventID string, processingErr error) error {
	errStr := ""
	if processingErr != nil {
		errStr = processingErr.Error()
	}
	if r.q.isMySQL {
		_, err := r.db.ExecContext(ctx, r.q.markEventDone, errStr, eventID)
		return err
	}
	_, err := r.db.ExecContext(ctx, r.q.markEventDone, eventID, errStr)
	return err
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (r *repository) getSubscriptionByID(ctx context.Context, id int64) (*Subscription, error) {
	return r.scanSubscription(r.db.QueryRowContext(ctx, r.q.findSubByID, id))
}

func (r *repository) getProductByID(ctx context.Context, id string) (*Product, error) {
	var p Product
	var metaBytes, featBytes []byte
	err := r.db.QueryRowContext(ctx, r.q.getProductByID, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.Active,
		&metaBytes, &featBytes, &p.StripeCreatedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("stripeflow: product not found")
		}
		return nil, err
	}
	if len(metaBytes) > 0 {
		m := json.RawMessage(metaBytes)
		p.Metadata = &m
	}
	if len(featBytes) > 0 {
		f := json.RawMessage(featBytes)
		p.Features = &f
	}
	return &p, nil
}

func (r *repository) deleteProduct(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete prices first
	if _, err := tx.ExecContext(ctx, r.q.deletePricesForProduct, id); err != nil {
		return err
	}
	// Delete product
	if _, err := tx.ExecContext(ctx, r.q.deleteProduct, id); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *repository) deleteAllProducts(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete all prices first
	if _, err := tx.ExecContext(ctx, r.q.deleteAllPrices); err != nil {
		return err
	}
	// Delete all products
	if _, err := tx.ExecContext(ctx, r.q.deleteAllProducts); err != nil {
		return err
	}

	return tx.Commit()
}
