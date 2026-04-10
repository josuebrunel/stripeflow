package models

import (
	"encoding/json"
	"time"
)

type Plan struct {
	ID              string          `json:"id" db:"id"`
	Name            string          `json:"name" db:"name"`
	Slug            string          `json:"slug" db:"slug"`
	StripeProductID string          `json:"stripe_product_id" db:"stripe_product_id"`
	StripePriceID   string          `json:"stripe_price_id" db:"stripe_price_id"`
	Description     *string         `json:"description" db:"description"`
	PriceUsd        int32           `json:"price_usd" db:"price_usd"`
	IsActive        bool            `json:"is_active" db:"is_active"`
	BillingCycle    string          `json:"billing_cycle" db:"billing_cycle"`
	Features        *json.RawMessage `json:"features" db:"features"`
	SortOrder       int32           `json:"sort_order" db:"sort_order"`
	MaxDescriptions int32           `json:"max_descriptions" db:"max_descriptions"`
	MaxPhotos       int32           `json:"max_photos" db:"max_photos"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

type Subscription struct {
	ID                   string    `json:"id" db:"id"`
	StripeCustomerID     string    `json:"stripe_customer_id" db:"stripe_customer_id"`
	StripeSubscriptionID string    `json:"stripe_subscription_id" db:"stripe_subscription_id"`
	StripePriceID        string    `json:"stripe_price_id" db:"stripe_price_id"`
	UserID               string    `json:"user_id" db:"user_id"`
	PlanName             string    `json:"plan_name" db:"plan_name"`
	Status               string    `json:"status" db:"status"`
	UsageDesc            int32     `json:"usage_desc" db:"usage_desc"`
	UsagePhotos          int32     `json:"usage_photos" db:"usage_photos"`
	DateStart            time.Time `json:"date_start" db:"date_start"`
	DateEnd              time.Time `json:"date_end" db:"date_end"`
	DateRenewal          time.Time `json:"date_renewal" db:"date_renewal"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time `json:"updated_at" db:"updated_at"`
}
