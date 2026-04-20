package stripeflow

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Plan represents a Stripe product/price synced to the local database.
type Plan struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Slug            string           `json:"slug"`
	StripeProductID string           `json:"stripe_product_id"`
	StripePriceID   string           `json:"stripe_price_id"`
	Description     *string          `json:"description"`
	PriceUsd        int32            `json:"price_usd"`
	IsActive        bool             `json:"is_active"`
	BillingCycle    string           `json:"billing_cycle"`
	Features        *json.RawMessage `json:"features"`
	SortOrder       int32            `json:"sort_order"`
	Metadata        *json.RawMessage `json:"metadata"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// Subscription represents a user's Stripe subscription synced to the local database.
type Subscription struct {
	ID                   string           `json:"id"`
	StripeCustomerID     string           `json:"stripe_customer_id"`
	StripeSubscriptionID string           `json:"stripe_subscription_id"`
	StripePriceID        string           `json:"stripe_price_id"`
	UserID               string           `json:"user_id"`
	PlanName             string           `json:"plan_name"`
	Status               string           `json:"status"`
	Metadata             *json.RawMessage `json:"metadata"`
	DateStart            time.Time        `json:"date_start"`
	DateEnd              time.Time        `json:"date_end"`
	DateRenewal          time.Time        `json:"date_renewal"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

// scanPlan scans a single Plan from a sql.Row or sql.Rows.
func scanPlan(sc interface{ Scan(...any) error }) (*Plan, error) {
	var p Plan
	var features, metadata sql.NullString
	err := sc.Scan(
		&p.ID, &p.Name, &p.Slug, &p.StripeProductID, &p.StripePriceID,
		&p.Description, &p.PriceUsd, &p.IsActive, &p.BillingCycle,
		&features, &p.SortOrder, &metadata, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if features.Valid {
		f := json.RawMessage(features.String)
		p.Features = &f
	}
	if metadata.Valid {
		m := json.RawMessage(metadata.String)
		p.Metadata = &m
	}
	return &p, nil
}

// scanSubscription scans a single Subscription from a sql.Row or sql.Rows.
func scanSubscription(sc interface{ Scan(...any) error }) (*Subscription, error) {
	var s Subscription
	var metadata sql.NullString
	err := sc.Scan(
		&s.ID, &s.StripeCustomerID, &s.StripeSubscriptionID, &s.StripePriceID,
		&s.UserID, &s.PlanName, &s.Status, &metadata,
		&s.DateStart, &s.DateEnd, &s.DateRenewal, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if metadata.Valid {
		m := json.RawMessage(metadata.String)
		s.Metadata = &m
	}
	return &s, nil
}
