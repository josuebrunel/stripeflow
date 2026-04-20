package stripeflow

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// queries holds all SQL statements for a given dialect.
type queries struct {
	upsertPlan       string
	findPlan         string
	getPlans         string
	upsertSub        string
	findSubByUser    string
	findSubByStripe  string
	deleteSub        string
	checkActiveSub   string
	hasReturning     bool
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

// --- Postgres queries ---

var pgQueries = queries{
	hasReturning: true,
	upsertPlan: `
		INSERT INTO stripeflow_plans
			(name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (stripe_price_id) DO UPDATE SET
			name=EXCLUDED.name, slug=EXCLUDED.slug, stripe_product_id=EXCLUDED.stripe_product_id,
			description=EXCLUDED.description, price_usd=EXCLUDED.price_usd, is_active=EXCLUDED.is_active,
			billing_cycle=EXCLUDED.billing_cycle, features=EXCLUDED.features, sort_order=EXCLUDED.sort_order,
			metadata=EXCLUDED.metadata, updated_at=CURRENT_TIMESTAMP
		RETURNING id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at`,
	findPlan: `SELECT id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at
		FROM stripeflow_plans WHERE stripe_price_id = $1`,
	getPlans: `SELECT id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at
		FROM stripeflow_plans WHERE is_active = true ORDER BY sort_order`,
	upsertSub: `
		INSERT INTO stripeflow_subscriptions
			(stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (stripe_customer_id, stripe_subscription_id) DO UPDATE SET
			stripe_price_id=EXCLUDED.stripe_price_id, plan_name=EXCLUDED.plan_name, status=EXCLUDED.status,
			metadata=EXCLUDED.metadata, date_start=EXCLUDED.date_start, date_end=EXCLUDED.date_end,
			date_renewal=EXCLUDED.date_renewal, updated_at=CURRENT_TIMESTAMP
		RETURNING id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at`,
	findSubByUser: `SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at
		FROM stripeflow_subscriptions WHERE user_id = $1 ORDER BY date_start DESC LIMIT 1`,
	findSubByStripe: `SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_subscription_id = $1 AND stripe_customer_id = $2`,
	deleteSub:      `DELETE FROM stripeflow_subscriptions WHERE id = $1`,
	checkActiveSub: `SELECT COUNT(id) FROM stripeflow_subscriptions WHERE user_id = $1 AND status IN ('active','trialing') AND date_renewal > $2`,
}

// --- MySQL queries ---

var myQueries = queries{
	hasReturning: false,
	upsertPlan: `
		INSERT INTO stripeflow_plans
			(id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			name=VALUES(name), slug=VALUES(slug), stripe_product_id=VALUES(stripe_product_id),
			description=VALUES(description), price_usd=VALUES(price_usd), is_active=VALUES(is_active),
			billing_cycle=VALUES(billing_cycle), features=VALUES(features), sort_order=VALUES(sort_order),
			metadata=VALUES(metadata), updated_at=CURRENT_TIMESTAMP`,
	findPlan: `SELECT id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at
		FROM stripeflow_plans WHERE stripe_price_id = ?`,
	getPlans: `SELECT id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at
		FROM stripeflow_plans WHERE is_active = true ORDER BY sort_order`,
	upsertSub: `
		INSERT INTO stripeflow_subscriptions
			(id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			stripe_price_id=VALUES(stripe_price_id), plan_name=VALUES(plan_name), status=VALUES(status),
			metadata=VALUES(metadata), date_start=VALUES(date_start), date_end=VALUES(date_end),
			date_renewal=VALUES(date_renewal), updated_at=CURRENT_TIMESTAMP`,
	findSubByUser: `SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at
		FROM stripeflow_subscriptions WHERE user_id = ? ORDER BY date_start DESC LIMIT 1`,
	findSubByStripe: `SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_subscription_id = ? AND stripe_customer_id = ?`,
	deleteSub:      `DELETE FROM stripeflow_subscriptions WHERE id = ?`,
	checkActiveSub: `SELECT COUNT(id) FROM stripeflow_subscriptions WHERE user_id = ? AND status IN ('active','trialing') AND date_renewal > ?`,
}

// --- SQLite queries ---

var slQueries = queries{
	hasReturning: true,
	upsertPlan: `
		INSERT INTO stripeflow_plans
			(id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (stripe_price_id) DO UPDATE SET
			name=EXCLUDED.name, slug=EXCLUDED.slug, stripe_product_id=EXCLUDED.stripe_product_id,
			description=EXCLUDED.description, price_usd=EXCLUDED.price_usd, is_active=EXCLUDED.is_active,
			billing_cycle=EXCLUDED.billing_cycle, features=EXCLUDED.features, sort_order=EXCLUDED.sort_order,
			metadata=EXCLUDED.metadata, updated_at=CURRENT_TIMESTAMP
		RETURNING id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at`,
	findPlan: `SELECT id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at
		FROM stripeflow_plans WHERE stripe_price_id = ?`,
	getPlans: `SELECT id, name, slug, stripe_product_id, stripe_price_id, description, price_usd, is_active, billing_cycle, features, sort_order, metadata, created_at, updated_at
		FROM stripeflow_plans WHERE is_active = true ORDER BY sort_order`,
	upsertSub: `
		INSERT INTO stripeflow_subscriptions
			(id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (stripe_customer_id, stripe_subscription_id) DO UPDATE SET
			stripe_price_id=EXCLUDED.stripe_price_id, plan_name=EXCLUDED.plan_name, status=EXCLUDED.status,
			metadata=EXCLUDED.metadata, date_start=EXCLUDED.date_start, date_end=EXCLUDED.date_end,
			date_renewal=EXCLUDED.date_renewal, updated_at=CURRENT_TIMESTAMP
		RETURNING id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at`,
	findSubByUser: `SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at
		FROM stripeflow_subscriptions WHERE user_id = ? ORDER BY date_start DESC LIMIT 1`,
	findSubByStripe: `SELECT id, stripe_customer_id, stripe_subscription_id, stripe_price_id, user_id, plan_name, status, metadata, date_start, date_end, date_renewal, created_at, updated_at
		FROM stripeflow_subscriptions WHERE stripe_subscription_id = ? AND stripe_customer_id = ?`,
	deleteSub:      `DELETE FROM stripeflow_subscriptions WHERE id = ?`,
	checkActiveSub: `SELECT COUNT(id) FROM stripeflow_subscriptions WHERE user_id = ? AND status IN ('active','trialing') AND date_renewal > ?`,
}

// --- Repository ---

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

func (r *repository) upsertPlan(ctx context.Context, plan *Plan) (*Plan, error) {
	var args []any
	if r.dialect == "postgres" {
		args = []any{plan.Name, plan.Slug, plan.StripeProductID, plan.StripePriceID,
			plan.Description, plan.PriceUsd, plan.IsActive, plan.BillingCycle,
			plan.Features, plan.SortOrder, plan.Metadata}
	} else {
		if plan.ID == "" {
			plan.ID = uuid.NewString()
		}
		args = []any{plan.ID, plan.Name, plan.Slug, plan.StripeProductID, plan.StripePriceID,
			plan.Description, plan.PriceUsd, plan.IsActive, plan.BillingCycle,
			plan.Features, plan.SortOrder, plan.Metadata}
	}

	if r.q.hasReturning {
		return scanPlan(r.db.QueryRowContext(ctx, r.q.upsertPlan, args...))
	}

	_, err := r.db.ExecContext(ctx, r.q.upsertPlan, args...)
	if err != nil {
		return nil, err
	}
	return r.findPlan(ctx, plan.StripePriceID)
}

func (r *repository) findPlan(ctx context.Context, priceID string) (*Plan, error) {
	return scanPlan(r.db.QueryRowContext(ctx, r.q.findPlan, priceID))
}

func (r *repository) getPlans(ctx context.Context) ([]Plan, error) {
	rows, err := r.db.QueryContext(ctx, r.q.getPlans)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *p)
	}
	return plans, rows.Err()
}

func (r *repository) upsertSubscription(ctx context.Context, sub *Subscription) (*Subscription, error) {
	var args []any
	if r.dialect == "postgres" {
		args = []any{sub.StripeCustomerID, sub.StripeSubscriptionID, sub.StripePriceID,
			sub.UserID, sub.PlanName, sub.Status, sub.Metadata,
			sub.DateStart, sub.DateEnd, sub.DateRenewal}
	} else {
		if sub.ID == "" {
			sub.ID = uuid.NewString()
		}
		args = []any{sub.ID, sub.StripeCustomerID, sub.StripeSubscriptionID, sub.StripePriceID,
			sub.UserID, sub.PlanName, sub.Status, sub.Metadata,
			sub.DateStart, sub.DateEnd, sub.DateRenewal}
	}

	if r.q.hasReturning {
		return scanSubscription(r.db.QueryRowContext(ctx, r.q.upsertSub, args...))
	}

	_, err := r.db.ExecContext(ctx, r.q.upsertSub, args...)
	if err != nil {
		return nil, err
	}
	return r.findSubscriptionByStripeID(ctx, sub.StripeSubscriptionID, sub.StripeCustomerID)
}

func (r *repository) findSubscriptionByUserID(ctx context.Context, userID string) (*Subscription, error) {
	return scanSubscription(r.db.QueryRowContext(ctx, r.q.findSubByUser, userID))
}

func (r *repository) findSubscriptionByStripeID(ctx context.Context, stripeSubID, stripeCustomerID string) (*Subscription, error) {
	return scanSubscription(r.db.QueryRowContext(ctx, r.q.findSubByStripe, stripeSubID, stripeCustomerID))
}

func (r *repository) deleteSubscription(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, r.q.deleteSub, id)
	return err
}

func (r *repository) checkActiveSubscription(ctx context.Context, userID string) (bool, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, r.q.checkActiveSub, userID, time.Now().UTC()).Scan(&count)
	return count > 0, err
}
