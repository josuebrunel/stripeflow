package stripeflow

import (
	"context"
	"errors"
	"net/http"
)

// contextKey is the unexported type used for context values set by stripeflow.
type contextKey int

const ctxKeySubscription contextKey = iota

// SubscriptionFromContext retrieves the Subscription stored in the request
// context by the RequireSubscription middleware.
func SubscriptionFromContext(ctx context.Context) (*Subscription, bool) {
	sub, ok := ctx.Value(ctxKeySubscription).(*Subscription)
	return sub, ok
}

// --------------------------------------------------------------------------
// DeniedFunc
// --------------------------------------------------------------------------

// DeniedFunc is called by middleware when access is denied.
// It should write an appropriate HTTP response and return.
// If nil in MiddlewareOptions, a default JSON response is used.
type DeniedFunc func(w http.ResponseWriter, r *http.Request, reason error)

func defaultDenied(w http.ResponseWriter, r *http.Request, reason error) {
	w.Header().Set("Content-Type", "application/json")
	var code int
	var msg string
	switch {
	case errors.Is(reason, ErrNoSubscription):
		code = http.StatusPaymentRequired
		msg = `{"error":"no_subscription","message":"A subscription is required to access this resource."}`
	case errors.Is(reason, ErrTrialExpired):
		code = http.StatusPaymentRequired
		msg = `{"error":"trial_expired","message":"Your trial has expired. Please subscribe to continue."}`
	case errors.Is(reason, ErrSubscriptionInactive):
		code = http.StatusPaymentRequired
		msg = `{"error":"subscription_inactive","message":"Your subscription is not active."}`
	case errors.Is(reason, ErrUsageLimitReached):
		code = http.StatusTooManyRequests
		msg = `{"error":"usage_limit_reached","message":"You have reached your usage limit for this billing period."}`
	default:
		code = http.StatusForbidden
		msg = `{"error":"forbidden","message":"Access denied."}`
	}
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

// --------------------------------------------------------------------------
// MiddlewareOptions
// --------------------------------------------------------------------------

// MiddlewareOptions customises the behaviour of subscription middleware.
type MiddlewareOptions struct {
	// OnDenied overrides the default HTTP response when access is denied.
	// If nil, a default JSON error response is used.
	OnDenied DeniedFunc

	// AllowTrialing permits requests from users in a valid (non-expired) trial.
	// Defaults to true when using RequireActiveOrTrial.
	AllowTrialing bool

	// CheckUsageLimit enables the usage-limit check for this specific route,
	// regardless of the global Config.UsageLimitEnabled setting.
	CheckUsageLimit bool
}

func (o *MiddlewareOptions) applyDefaults() {
	if o.OnDenied == nil {
		o.OnDenied = defaultDenied
	}
}

// --------------------------------------------------------------------------
// Middleware
// --------------------------------------------------------------------------

// RequireSubscription is an http.Handler middleware that rejects requests from
// users without an active subscription (or valid trial, depending on options).
//
// The resolved *Subscription is stored in the context and accessible via
// SubscriptionFromContext. Config.GetUserID must be set.
//
//	mux.Handle("/app/", sf.RequireSubscription(appHandler))
//	mux.Handle("/api/", sf.RequireSubscription(apiHandler, stripeflow.MiddlewareOptions{
//	    AllowTrialing: false,
//	    CheckUsageLimit: true,
//	}))
func (c *Client) RequireSubscription(next http.Handler, opts ...MiddlewareOptions) http.Handler {
	opt := MiddlewareOptions{AllowTrialing: true}
	if len(opts) > 0 {
		opt = opts[0]
	}
	opt.applyDefaults()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, reason := c.checkSubscription(r, opt)
		if reason != nil {
			opt.OnDenied(w, r, reason)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeySubscription, sub)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireActiveOrTrial allows users who are actively subscribed OR in a valid trial.
func (c *Client) RequireActiveOrTrial(next http.Handler) http.Handler {
	return c.RequireSubscription(next, MiddlewareOptions{AllowTrialing: true})
}

// RequireActiveSubscription requires a fully paid (non-trial) active subscription.
func (c *Client) RequireActiveSubscription(next http.Handler) http.Handler {
	return c.RequireSubscription(next, MiddlewareOptions{AllowTrialing: false})
}

// checkSubscription validates subscription status and returns either a valid
// Subscription or the reason access was denied.
func (c *Client) checkSubscription(r *http.Request, opt MiddlewareOptions) (*Subscription, error) {
	if c.cfg.GetUserID == nil {
		panic("stripeflow: Config.GetUserID must be set to use middleware")
	}

	userID, err := c.cfg.GetUserID(r)
	if err != nil || userID == "" {
		return nil, ErrNoSubscription
	}

	sub, err := c.repo.getSubscriptionByUserID(r.Context(), userID)
	if err != nil {
		return nil, ErrNoSubscription
	}

	// Trial checks.
	if sub.Status == StatusTrialing {
		if sub.TrialExpired() {
			return nil, ErrTrialExpired
		}
		if !opt.AllowTrialing {
			return nil, ErrTrialExpired
		}
	} else {
		if !sub.IsActive() {
			return nil, ErrSubscriptionInactive
		}
	}

	// Usage limit check.
	checkUsage := c.cfg.UsageLimitEnabled || opt.CheckUsageLimit
	if checkUsage && sub.UsageLimitReached() {
		return nil, ErrUsageLimitReached
	}

	return sub, nil
}

// --------------------------------------------------------------------------
// Usage helpers
// --------------------------------------------------------------------------

// IncrementUsage adds delta to the user's usage counter and returns the new total.
// Typically called after a successful API operation.
//
//	newCount, err := sf.IncrementUsage(ctx, userID, 1)
func (c *Client) IncrementUsage(ctx context.Context, userID string, delta int64) (int64, error) {
	return c.repo.incrementUsage(ctx, userID, delta)
}

// SetUsageLimit sets or removes the usage cap for a user.
// Pass nil to remove the limit (unlimited).
//
//	err := sf.SetUsageLimit(ctx, userID, stripeflow.Int64Ptr(1000))
func (c *Client) SetUsageLimit(ctx context.Context, userID string, limit *int64) error {
	return c.repo.setUsageLimit(ctx, userID, limit)
}

// ResetUsage zeroes the usage counter for a user.
// Typically called at the start of each billing period.
func (c *Client) ResetUsage(ctx context.Context, userID string) error {
	return c.repo.resetUsage(ctx, userID)
}
