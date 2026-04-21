package stripeflow

import (
	"encoding/json"
	"time"
)

// Subscription represents a user's Stripe subscription state as stored locally.
type Subscription struct {
	ID                   int64
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
	UsageCount           int64
	UsageLimit           *int64
	Metadata             *json.RawMessage
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// IsActive reports whether the subscription is in an active or trialing state.
func (s *Subscription) IsActive() bool {
	return s.Status.IsActive()
}

// TrialExpired reports whether the user's trial period has ended.
func (s *Subscription) TrialExpired() bool {
	if s.Status != StatusTrialing {
		return false
	}
	if s.TrialEndsAt == nil {
		return false
	}
	return time.Now().After(*s.TrialEndsAt)
}

// UsageLimitReached reports whether the user has exhausted their usage allowance.
func (s *Subscription) UsageLimitReached() bool {
	if s.UsageLimit == nil {
		return false
	}
	return s.UsageCount >= *s.UsageLimit
}

// Product mirrors a Stripe product stored locally.
type Product struct {
	ID              string
	Name            string
	Description     string
	Active          bool
	Metadata        *json.RawMessage
	Features        *json.RawMessage
	StripeCreatedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Price mirrors a Stripe price stored locally.
type Price struct {
	ID                string
	ProductID         string
	Currency          string
	UnitAmount        *int64
	RecurringInterval string
	RecurringCount    *int
	Active            bool
	Metadata          *json.RawMessage
	StripeCreatedAt   *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
