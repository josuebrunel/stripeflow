package stripeflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/webhook"
)

const (
	statusActive   = "active"
	statusTrialing = "trialing"

	checkoutSuccess = "success"
	checkoutFailed  = "failed"

	metadataUserID    = "user_id"
	metadataUserEmail = "user_email"
	metadataPlanID    = "plan_id"
	metadataPlanName  = "plan_name"
)

// UserResolver is implemented by the consumer to tie Stripe subscriptions
// to your application's user model.
type UserResolver interface {
	// GetUserID returns the authenticated user's ID from the request context.
	GetUserID(ctx context.Context) (string, error)
	// GetUserEmail returns the authenticated user's email from the request context.
	GetUserEmail(ctx context.Context) (string, error)
	// FindUserIDByEmail looks up a user ID by email address (used by webhook handlers).
	FindUserIDByEmail(ctx context.Context, email string) (string, error)
}

// Handler returns an http.Handler that serves the checkout, portal, and webhook endpoints.
//
//	POST /checkout  — creates a Stripe Checkout session
//	GET  /portal    — creates a Stripe Billing Portal session
//	POST /webhook   — receives Stripe webhook events
func (sf *StripeFlow) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /checkout", sf.handleCheckout)
	mux.HandleFunc("GET /portal", sf.handlePortal)
	mux.HandleFunc("POST /webhook", sf.handleWebhook)
	return mux
}

// RequireSubscription returns middleware that checks for an active subscription.
// If the user has no active subscription, the fallback handler is called instead.
func (sf *StripeFlow) RequireSubscription(fallback http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := sf.resolver.GetUserID(r.Context())
			if err != nil || userID == "" {
				slog.Error("RequireSubscription: could not get user", "error", err)
				fallback(w, r)
				return
			}

			active, err := sf.repo.checkActiveSubscription(r.Context(), userID)
			if err != nil || !active {
				slog.Debug("RequireSubscription: no active subscription", "user_id", userID, "error", err)
				fallback(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// UsageFunc is called by RequireUsage to determine if a user is within their
// plan's usage limits. It receives the user's active subscription and the
// associated plan (including its Metadata from Stripe).
//
// Return nil to allow the request, or a non-nil error to deny it.
//
// Example: check API call count against plan metadata "max_api_calls":
//
//	func checkAPICalls(ctx context.Context, sub *stripeflow.Subscription, plan *stripeflow.Plan) error {
//	    limit := extractLimit(plan.Metadata, "max_api_calls") // your logic
//	    used := countAPICalls(ctx, sub.UserID)                // your logic
//	    if used >= limit {
//	        return fmt.Errorf("API call limit reached (%d/%d)", used, limit)
//	    }
//	    return nil
//	}
type UsageFunc func(ctx context.Context, subscription *Subscription, plan *Plan) error

// RequireUsage returns middleware that checks usage-based limits.
// It resolves the user's active subscription and plan, then calls the provided
// check function. If check returns a non-nil error, the fallback handler is invoked.
func (sf *StripeFlow) RequireUsage(check UsageFunc, fallback http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := sf.resolver.GetUserID(r.Context())
			if err != nil || userID == "" {
				slog.Error("RequireUsage: could not get user", "error", err)
				fallback(w, r)
				return
			}

			sub, err := sf.repo.findSubscriptionByUserID(r.Context(), userID)
			if err != nil || sub == nil {
				slog.Debug("RequireUsage: no subscription", "user_id", userID, "error", err)
				fallback(w, r)
				return
			}

			plan, err := sf.repo.findPlan(r.Context(), sub.StripePriceID)
			if err != nil || plan == nil {
				slog.Error("RequireUsage: plan not found", "price_id", sub.StripePriceID, "error", err)
				fallback(w, r)
				return
			}

			if err := check(r.Context(), sub, plan); err != nil {
				slog.Debug("RequireUsage: check failed", "user_id", userID, "error", err)
				fallback(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CheckUsage is a non-middleware version of RequireUsage for use in business logic.
// It resolves the user's subscription and plan, then calls the check function.
func (sf *StripeFlow) CheckUsage(ctx context.Context, userID string, check UsageFunc) error {
	sub, err := sf.repo.findSubscriptionByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("no subscription found: %w", err)
	}

	plan, err := sf.repo.findPlan(ctx, sub.StripePriceID)
	if err != nil {
		return fmt.Errorf("plan not found: %w", err)
	}

	return check(ctx, sub, plan)
}

func (sf *StripeFlow) handleCheckout(w http.ResponseWriter, r *http.Request) {
	userID, err := sf.resolver.GetUserID(r.Context())
	if err != nil || userID == "" {
		slog.Error("checkout: could not get user", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	email, err := sf.resolver.GetUserEmail(r.Context())
	if err != nil || email == "" {
		slog.Error("checkout: could not get email", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	planID := r.FormValue("plan_id")
	if planID == "" {
		http.Error(w, "Missing plan_id", http.StatusBadRequest)
		return
	}

	plan, err := sf.repo.findPlan(r.Context(), planID)
	if err != nil || plan == nil {
		slog.Error("checkout: plan not found", "plan_id", planID, "error", err)
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	metadata := map[string]string{
		metadataUserID:    userID,
		metadataUserEmail: email,
		metadataPlanID:    planID,
		metadataPlanName:  plan.Name,
	}

	params := &stripe.CheckoutSessionParams{
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(plan.StripePriceID), Quantity: stripe.Int64(1)},
		},
		Mode:          stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		AutomaticTax:  &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
		CustomerEmail: stripe.String(email),
		SuccessURL:    stripe.String(sf.redirectURL(checkoutSuccess)),
		CancelURL:     stripe.String(sf.redirectURL(checkoutFailed)),
		Metadata:      metadata,
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: metadata,
		},
	}

	sess, err := checkoutsession.New(params)
	if err != nil {
		slog.Error("checkout: failed to create session", "error", err)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, sess.URL, http.StatusSeeOther)
}

func (sf *StripeFlow) handlePortal(w http.ResponseWriter, r *http.Request) {
	userID, err := sf.resolver.GetUserID(r.Context())
	if err != nil || userID == "" {
		slog.Error("portal: could not get user", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	sub, err := sf.repo.findSubscriptionByUserID(r.Context(), userID)
	if err != nil || sub == nil {
		slog.Error("portal: no subscription found", "user_id", userID, "error", err)
		http.Redirect(w, r, "/account/?action=subscription.pricing", http.StatusSeeOther)
		return
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sub.StripeCustomerID),
		ReturnURL: stripe.String(sf.redirectURL(checkoutSuccess)),
	}
	sess, err := session.New(params)
	if err != nil {
		slog.Error("portal: failed to create session", "error", err)
		http.Redirect(w, r, "/account/", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, sess.URL, http.StatusSeeOther)
}

func (sf *StripeFlow) handleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("webhook: failed to read body", "error", err)
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}

	event, err := webhook.ConstructEventWithOptions(
		payload, r.Header.Get("Stripe-Signature"), sf.webhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
	)
	if err != nil {
		slog.Error("webhook: failed to verify signature", "error", err)
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	slog.Debug("webhook: received event", "type", event.Type)

	switch event.Type {
	case stripe.EventTypeCustomerSubscriptionUpdated:
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			slog.Error("webhook: unmarshal subscription", "error", err)
			break
		}
		if err := sf.handleSubscriptionUpdated(r.Context(), &subscription); err != nil {
			slog.Error("webhook: subscription updated", "error", err)
		}

	case stripe.EventTypeCustomerSubscriptionDeleted:
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			slog.Error("webhook: unmarshal subscription", "error", err)
			break
		}
		if err := sf.handleSubscriptionDeleted(r.Context(), &subscription); err != nil {
			slog.Error("webhook: subscription deleted", "error", err)
		}

	case stripe.EventTypeInvoicePaid:
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			slog.Error("webhook: unmarshal invoice", "error", err)
			http.Error(w, "Error processing event", http.StatusInternalServerError)
			return
		}

		userID, err := sf.resolver.FindUserIDByEmail(r.Context(), invoice.CustomerEmail)
		if err != nil {
			slog.Error("webhook: user not found by email", "email", invoice.CustomerEmail, "error", err)
			w.WriteHeader(http.StatusOK) // don't retry
			return
		}

		if err := sf.handleInvoicePaid(r.Context(), &invoice, userID); err != nil {
			slog.Error("webhook: invoice paid", "error", err)
		}

	case stripe.EventTypeInvoicePaymentFailed:
		slog.Info("webhook: invoice payment failed", "invoice", string(event.Data.Raw))

	default:
		slog.Debug("webhook: unhandled event", "type", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

func (sf *StripeFlow) redirectURL(mode string) string {
	u, err := url.Parse(sf.checkoutRedirect)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("checkout", mode)
	u.RawQuery = q.Encode()
	return u.String()
}

// slugify converts a string to a URL-friendly slug.
func slugify(s string) string {
	result := make([]byte, 0, len(s))
	lastWasDash := false
	for _, b := range []byte(s) {
		switch {
		case b >= 'a' && b <= 'z', b >= '0' && b <= '9':
			result = append(result, b)
			lastWasDash = false
		case b >= 'A' && b <= 'Z':
			result = append(result, b+32) // lowercase
			lastWasDash = false
		case b == ' ' || b == '-' || b == '_':
			if !lastWasDash && len(result) > 0 {
				result = append(result, '-')
				lastWasDash = true
			}
		}
	}
	if len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}
	return string(result)
}

// priceDisplay is a helper to format a stripe price amount for metadata.
func priceDisplay(amount int32) string {
	return strconv.Itoa(int(amount))
}
