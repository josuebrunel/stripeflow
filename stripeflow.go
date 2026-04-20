// Package stripeflow provides a pluggable library for integrating Stripe
// subscriptions into Go applications. It handles checkout sessions, billing
// portal access, webhook processing, and subscription state management.
//
// Supports PostgreSQL, MySQL, and SQLite.
package stripeflow

import (
	"context"
	"database/sql"

	"github.com/stripe/stripe-go/v82"
)

// Config holds the configuration for StripeFlow.
type Config struct {
	// Dialect specifies the SQL dialect: "postgres", "mysql", "sqlite", or "sqlite3".
	Dialect string
	// DB is the database connection to use.
	DB *sql.DB
	// StripeSecretKey is your Stripe secret API key (sk_...).
	StripeSecretKey string
	// WebhookSecret is the Stripe webhook signing secret (whsec_...).
	WebhookSecret string
	// RedirectURL is the URL to redirect to after checkout (success/failure).
	RedirectURL string
}

// StripeFlow is the main entry point for the library.
type StripeFlow struct {
	repo             *repository
	resolver         UserResolver
	webhookSecret    string
	checkoutRedirect string
}

// New creates a new StripeFlow instance. The stripe API key is set globally
// once at initialization.
func New(cfg Config, resolver UserResolver) (*StripeFlow, error) {
	repo, err := newRepository(cfg.DB, cfg.Dialect)
	if err != nil {
		return nil, err
	}

	// Set the Stripe API key once.
	stripe.Key = cfg.StripeSecretKey

	return &StripeFlow{
		repo:             repo,
		resolver:         resolver,
		webhookSecret:    cfg.WebhookSecret,
		checkoutRedirect: cfg.RedirectURL,
	}, nil
}

// --- Public convenience methods ---

// GetPlans returns all active plans, ordered by sort_order.
func (sf *StripeFlow) GetPlans(ctx context.Context) ([]Plan, error) {
	return sf.repo.getPlans(ctx)
}

// FindPlan looks up a plan by its Stripe price ID.
func (sf *StripeFlow) FindPlan(ctx context.Context, priceID string) (*Plan, error) {
	return sf.repo.findPlan(ctx, priceID)
}

// GetSubscription returns the most recent subscription for a user.
func (sf *StripeFlow) GetSubscription(ctx context.Context, userID string) (*Subscription, error) {
	return sf.repo.findSubscriptionByUserID(ctx, userID)
}

// HasActiveSubscription checks if a user has an active or trialing subscription.
func (sf *StripeFlow) HasActiveSubscription(ctx context.Context, userID string) (bool, error) {
	return sf.repo.checkActiveSubscription(ctx, userID)
}
