// Package stripeflow provides a pluggable Go library for integrating Stripe
// subscriptions into your application. It focuses on billing portal access,
// webhook processing, subscription state management, and product
// catalogue syncing — with support for PostgreSQL, MySQL, and SQLite.
//
// Quick start:
//
//	sf, err := stripeflow.New(stripeflow.Config{
//	    Dialect:         stripeflow.Postgres,
//	    DB:              db,
//	    StripeSecretKey: "sk_live_...",
//	    WebhookSecret:   "whsec_...",
//	    GetUserID: func(r *http.Request) (string, error) {
//	        return sessionUserID(r), nil
//	    },
//	})
//
//	// Register webhook handler
//	http.Handle("/stripe/webhook", sf.WebhookHandler())
//
//	// Protect routes
//	http.Handle("/app/", sf.RequireActiveOrTrial(appHandler))
package stripeflow

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/stripe/stripe-go/v82"
)

// Sentinel errors returned by middleware and programmatic helpers.
var (
	ErrNoSubscription       = errors.New("stripeflow: no subscription found")
	ErrSubscriptionInactive = errors.New("stripeflow: subscription is not active")
	ErrUsageLimitReached    = errors.New("stripeflow: usage limit reached")
	ErrTrialExpired         = errors.New("stripeflow: trial has expired")
)

// Dialect specifies the SQL dialect.
type Dialect string

const (
	Postgres Dialect = "postgres"
	MySQL    Dialect = "mysql"
	SQLite   Dialect = "sqlite"
)

// SubscriptionStatus mirrors Stripe's subscription statuses plus internal sentinels.
type SubscriptionStatus string

const (
	StatusActive            SubscriptionStatus = "active"
	StatusTrialing          SubscriptionStatus = "trialing"
	StatusPastDue           SubscriptionStatus = "past_due"
	StatusCanceled          SubscriptionStatus = "canceled"
	StatusIncomplete        SubscriptionStatus = "incomplete"
	StatusIncompleteExpired SubscriptionStatus = "incomplete_expired"
	StatusUnpaid            SubscriptionStatus = "unpaid"
	StatusPaused            SubscriptionStatus = "paused"
	// StatusNone means no Stripe subscription exists yet for this user.
	StatusNone SubscriptionStatus = "none"
)

// IsActive reports whether the status is billable / accessible.
func (s SubscriptionStatus) IsActive() bool {
	return s == StatusActive || s == StatusTrialing
}

// Config holds all configuration needed to initialise a StripeFlow client.
type Config struct {
	// Dialect specifies the SQL dialect (Postgres, MySQL, SQLite).
	Dialect Dialect

	// DB is the *sql.DB connection to use. stripeflow manages its own tables
	// under the "stripeflow_" namespace.
	DB *sql.DB

	// StripeSecretKey is your Stripe secret API key (sk_live_... or sk_test_...).
	StripeSecretKey string

	// WebhookSecret is the signing secret for your Stripe webhook endpoint (whsec_...).
	WebhookSecret string

	// GetUserID extracts the authenticated user's identifier from an HTTP request.
	// Required when using any middleware. Typically reads a JWT or session cookie.
	GetUserID func(r *http.Request) (string, error)

	// OnEvent is an optional hook called after every successfully processed webhook
	// event. Useful for cache invalidation, audit logging, etc.
	OnEvent func(event *stripe.Event)

	// TrialDays sets the default number of free trial days for new subscriptions.
	// Can be overridden per-checkout via CheckoutParams.TrialDays. Zero = no trial.
	TrialDays int64

	// UsageLimitEnabled toggles the built-in usage-limit check globally.
	// When true, middleware will deny requests once usage_count >= usage_limit.
	UsageLimitEnabled bool
}

func (c *Config) defaults() {
	if c.Dialect == "" {
		c.Dialect = Postgres
	}
}

func (c *Config) validate() error {
	if c.StripeSecretKey == "" {
		return fmt.Errorf("stripeflow: StripeSecretKey is required")
	}
	if c.WebhookSecret == "" {
		return fmt.Errorf("stripeflow: WebhookSecret is required")
	}
	if c.DB == nil {
		return fmt.Errorf("stripeflow: DB is required")
	}
	return nil
}

// Client is the main stripeflow object. Create one via New().
type Client struct {
	cfg  Config
	repo *repository
}

// New creates and initialises a stripeflow Client.
// The Stripe API key is set globally at initialisation time.
func New(cfg Config) (*Client, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	stripe.Key = cfg.StripeSecretKey

	repo, err := newRepository(cfg.DB, string(cfg.Dialect))
	if err != nil {
		return nil, err
	}

	return &Client{
		cfg:  cfg,
		repo: repo,
	}, nil
}

// Int64Ptr is a convenience helper that returns a pointer to an int64 value.
func Int64Ptr(v int64) *int64 { return &v }
