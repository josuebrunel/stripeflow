package stripeflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/billingportal/session"
	stripeCheckout "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

// --------------------------------------------------------------------------
// Checkout
// --------------------------------------------------------------------------

// CheckoutParams holds everything needed to create a Checkout session.
type CheckoutParams struct {
	// UserID is your internal user identifier – stored in the subscription table.
	UserID string

	// UserEmail is optional; pre-fills the email in Stripe's checkout form.
	UserEmail string

	// PriceID is the Stripe price ID (price_...) the user is subscribing to.
	PriceID string

	// SuccessURL is where Stripe redirects after a successful payment.
	SuccessURL string

	// CancelURL is where Stripe redirects when the user cancels.
	CancelURL string

	// Quantity defaults to 1.
	Quantity int64

	// AllowPromotionCodes enables the coupon/promo-code input on checkout.
	AllowPromotionCodes bool

	// TrialDays overrides Config.TrialDays for this specific checkout.
	// nil means use the global default; 0 means no trial.
	TrialDays *int64

	// Metadata is forwarded to the Stripe Session.
	Metadata map[string]string
}

// CheckoutResult is returned from CreateCheckout.
type CheckoutResult struct {
	// URL is the Stripe-hosted checkout page – redirect the user here.
	URL string
	// SessionID is the Stripe checkout session ID.
	SessionID string
	// CustomerID is the Stripe customer ID (new or existing).
	CustomerID string
}

// CreateCheckout creates a Stripe Checkout Session for a subscription and
// returns the URL to redirect the user to. If the user already has a Stripe
// customer ID it is reused; otherwise a new customer is created.
func (c *Client) CreateCheckout(ctx context.Context, p CheckoutParams) (*CheckoutResult, error) {
	if p.UserID == "" {
		return nil, fmt.Errorf("stripeflow: UserID is required")
	}
	if p.PriceID == "" {
		return nil, fmt.Errorf("stripeflow: PriceID is required")
	}
	if p.SuccessURL == "" || p.CancelURL == "" {
		return nil, fmt.Errorf("stripeflow: SuccessURL and CancelURL are required")
	}
	if p.Quantity <= 0 {
		p.Quantity = 1
	}

	// Ensure a subscription row exists so we can store the customer ID.
	if err := c.repo.createEmptySubscription(ctx, p.UserID); err != nil {
		return nil, fmt.Errorf("stripeflow: ensure subscription row: %w", err)
	}

	customerID, err := c.ensureCustomer(ctx, p.UserID, p.UserEmail, p.Metadata)
	if err != nil {
		return nil, err
	}

	params := &stripe.CheckoutSessionParams{
		Customer:   stripe.String(customerID),
		SuccessURL: stripe.String(p.SuccessURL),
		CancelURL:  stripe.String(p.CancelURL),
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(p.PriceID), Quantity: stripe.Int64(p.Quantity)},
		},
	}

	if p.AllowPromotionCodes {
		params.AllowPromotionCodes = stripe.Bool(true)
	}

	// Trial days: param override → global config.
	trialDays := c.cfg.TrialDays
	if p.TrialDays != nil {
		trialDays = *p.TrialDays
	}
	if trialDays > 0 {
		params.SubscriptionData = &stripe.CheckoutSessionSubscriptionDataParams{
			TrialPeriodDays: stripe.Int64(trialDays),
		}
	}

	// Always stamp stripeflow_user_id so webhooks can resolve back to the user.
	params.Metadata = make(map[string]string, len(p.Metadata)+1)
	for k, v := range p.Metadata {
		params.Metadata[k] = v
	}
	params.Metadata["stripeflow_user_id"] = p.UserID

	sess, err := stripeCheckout.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripeflow: create checkout session: %w", err)
	}

	return &CheckoutResult{
		URL:        sess.URL,
		SessionID:  sess.ID,
		CustomerID: customerID,
	}, nil
}

// ensureCustomer returns the existing Stripe customer ID for the user, or
// creates a new one and persists it.
func (c *Client) ensureCustomer(ctx context.Context, userID, email string, meta map[string]string) (string, error) {
	sub, err := c.repo.getSubscriptionByUserID(ctx, userID)
	if err == nil && sub.StripeCustomerID != "" {
		return sub.StripeCustomerID, nil
	}

	params := &stripe.CustomerParams{
		Metadata: map[string]string{"stripeflow_user_id": userID},
	}
	if email != "" {
		params.Email = stripe.String(email)
	}
	for k, v := range meta {
		params.Metadata[k] = v
	}

	cust, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("stripeflow: create stripe customer: %w", err)
	}

	if err := c.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:           userID,
		StripeCustomerID: cust.ID,
		Status:           StatusNone,
	}); err != nil {
		return "", fmt.Errorf("stripeflow: store customer id: %w", err)
	}
	return cust.ID, nil
}

// --------------------------------------------------------------------------
// Billing Portal
// --------------------------------------------------------------------------

// PortalParams holds options for creating a Billing Portal session.
type PortalParams struct {
	// UserID is your internal user identifier.
	UserID string
	// ReturnURL is where the customer lands after leaving the portal.
	ReturnURL string
}

// CreatePortalSession creates a Stripe Billing Portal session so the user can
// manage their subscription, update payment methods, and download invoices.
// Returns the portal URL to redirect the user to.
func (c *Client) CreatePortalSession(ctx context.Context, p PortalParams) (string, error) {
	sub, err := c.repo.getSubscriptionByUserID(ctx, p.UserID)
	if err != nil {
		return "", fmt.Errorf("stripeflow: get subscription: %w", err)
	}
	if sub.StripeCustomerID == "" {
		return "", fmt.Errorf("stripeflow: user %s has no Stripe customer ID", p.UserID)
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sub.StripeCustomerID),
		ReturnURL: stripe.String(p.ReturnURL),
	}
	sess, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("stripeflow: create portal session: %w", err)
	}
	return sess.URL, nil
}

// --------------------------------------------------------------------------
// Convenience Handler (thin mux)
// --------------------------------------------------------------------------

// Handler returns an http.Handler that mounts thin checkout, portal, and
// webhook routes. For full control over routing, call CreateCheckout,
// CreatePortalSession, and WebhookHandler directly instead.
//
//	POST /checkout  — creates a Stripe Checkout session, redirects to Stripe
//	GET  /portal    — creates a Billing Portal session, redirects to Stripe
//	POST /webhook   — receives and processes Stripe webhook events
func (c *Client) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /checkout", c.handleCheckoutForm)
	mux.HandleFunc("GET /portal", c.handlePortalRedirect)
	mux.Handle("POST /webhook", c.WebhookHandler())
	return mux
}

// handleCheckoutForm is the form-based checkout handler. Reads plan_id and
// redirect_url from the form, then redirects to Stripe.
func (c *Client) handleCheckoutForm(w http.ResponseWriter, r *http.Request) {
	if c.cfg.GetUserID == nil {
		http.Error(w, "stripeflow: GetUserID not configured", http.StatusInternalServerError)
		return
	}
	userID, err := c.cfg.GetUserID(r)
	if err != nil || userID == "" {
		slog.Error("checkout: could not get user", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	priceID := r.FormValue("price_id")
	if priceID == "" {
		http.Error(w, "Missing price_id", http.StatusBadRequest)
		return
	}
	successURL := r.FormValue("success_url")
	cancelURL := r.FormValue("cancel_url")

	result, err := c.CreateCheckout(r.Context(), CheckoutParams{
		UserID:     userID,
		PriceID:    priceID,
		SuccessURL: successURL,
		CancelURL:  cancelURL,
	})
	if err != nil {
		slog.Error("checkout: create session failed", "error", err)
		http.Error(w, "Failed to create checkout session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, result.URL, http.StatusSeeOther)
}

func (c *Client) handlePortalRedirect(w http.ResponseWriter, r *http.Request) {
	if c.cfg.GetUserID == nil {
		http.Error(w, "stripeflow: GetUserID not configured", http.StatusInternalServerError)
		return
	}
	userID, err := c.cfg.GetUserID(r)
	if err != nil || userID == "" {
		slog.Error("portal: could not get user", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	returnURL := r.FormValue("return_url")
	url, err := c.CreatePortalSession(r.Context(), PortalParams{
		UserID:    userID,
		ReturnURL: returnURL,
	})
	if err != nil {
		slog.Error("portal: create session failed", "error", err)
		http.Error(w, "Failed to create portal session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// --------------------------------------------------------------------------
// Webhook handler
// --------------------------------------------------------------------------

// WebhookHandler returns an http.Handler that verifies and processes Stripe
// webhook events. Mount it at the endpoint configured in the Stripe dashboard.
//
//	http.Handle("/stripe/webhook", sf.WebhookHandler())
func (c *Client) WebhookHandler() http.Handler {
	return http.HandlerFunc(c.handleWebhook)
}

func (c *Client) handleWebhook(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEventWithOptions(
		payload, r.Header.Get("Stripe-Signature"), c.cfg.WebhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
	)
	if err != nil {
		slog.Error("webhook: signature verification failed", "error", err)
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Idempotency guard.
	already, err := c.repo.markEventProcessing(ctx, event.ID, string(event.Type))
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if already {
		w.WriteHeader(http.StatusOK)
		return
	}

	processingErr := c.dispatchEvent(ctx, &event)
	_ = c.repo.markEventDone(ctx, event.ID, processingErr)

	if processingErr != nil {
		slog.Error("webhook: processing error", "type", event.Type, "error", processingErr)
		http.Error(w, processingErr.Error(), http.StatusInternalServerError)
		return
	}

	if c.cfg.OnEvent != nil {
		c.cfg.OnEvent(&event)
	}

	w.WriteHeader(http.StatusOK)
}

func (c *Client) dispatchEvent(ctx context.Context, event *stripe.Event) error {
	slog.Debug("webhook: dispatching event", "type", event.Type)
	switch event.Type {

	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return fmt.Errorf("unmarshal checkout.session.completed: %w", err)
		}
		return c.onCheckoutCompleted(ctx, &sess)

	case "customer.subscription.created", "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return fmt.Errorf("unmarshal subscription event: %w", err)
		}
		return c.onSubscriptionUpdated(ctx, &sub)

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return fmt.Errorf("unmarshal subscription.deleted: %w", err)
		}
		return c.onSubscriptionDeleted(ctx, &sub)

	case "customer.subscription.trial_will_end":
		// Informational – hook via OnEvent for reminder emails.
		return nil

	case "invoice.payment_succeeded":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return fmt.Errorf("unmarshal invoice.payment_succeeded: %w", err)
		}
		return c.onInvoicePaymentSucceeded(ctx, &inv)

	case "invoice.payment_failed":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return fmt.Errorf("unmarshal invoice.payment_failed: %w", err)
		}
		return c.onInvoicePaymentFailed(ctx, &inv)

	case "product.created", "product.updated":
		var prod stripe.Product
		if err := json.Unmarshal(event.Data.Raw, &prod); err != nil {
			return fmt.Errorf("unmarshal product event: %w", err)
		}
		return c.onProductUpserted(ctx, &prod)

	case "product.deleted":
		var prod stripe.Product
		if err := json.Unmarshal(event.Data.Raw, &prod); err != nil {
			return fmt.Errorf("unmarshal product.deleted: %w", err)
		}
		prod.Active = false
		return c.onProductUpserted(ctx, &prod)

	case "price.created", "price.updated":
		var p stripe.Price
		if err := json.Unmarshal(event.Data.Raw, &p); err != nil {
			return fmt.Errorf("unmarshal price event: %w", err)
		}
		return c.onPriceUpserted(ctx, &p)

	case "price.deleted":
		var p stripe.Price
		if err := json.Unmarshal(event.Data.Raw, &p); err != nil {
			return fmt.Errorf("unmarshal price.deleted: %w", err)
		}
		p.Active = false
		return c.onPriceUpserted(ctx, &p)
	}

	slog.Debug("webhook: unhandled event type", "type", event.Type)
	return nil
}

// --------------------------------------------------------------------------
// Event handlers
// --------------------------------------------------------------------------

func (c *Client) onCheckoutCompleted(ctx context.Context, sess *stripe.CheckoutSession) error {
	if sess.Mode != stripe.CheckoutSessionModeSubscription {
		return nil
	}

	userID := sess.Metadata["stripeflow_user_id"]
	if userID == "" && sess.Customer != nil {
		if existing, err := c.repo.getSubscriptionByCustomerID(ctx, sess.Customer.ID); err == nil {
			userID = existing.UserID
		}
	}
	if userID == "" {
		return fmt.Errorf("stripeflow: checkout.session.completed missing stripeflow_user_id")
	}

	custID := ""
	if sess.Customer != nil {
		custID = sess.Customer.ID
	}
	subID := ""
	if sess.Subscription != nil {
		subID = sess.Subscription.ID
	}

	return c.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               userID,
		StripeCustomerID:     custID,
		StripeSubscriptionID: subID,
		Status:               StatusActive,
	})
}

func (c *Client) onSubscriptionUpdated(ctx context.Context, sub *stripe.Subscription) error {
	existing, err := c.repo.getSubscriptionByCustomerID(ctx, sub.Customer.ID)
	if err != nil {
		return fmt.Errorf("stripeflow: subscription updated but customer unknown: %w", err)
	}

	p := upsertSubParams{
		UserID:               existing.UserID,
		StripeCustomerID:     sub.Customer.ID,
		StripeSubscriptionID: sub.ID,
		Status:               SubscriptionStatus(sub.Status),
	}

	if sub.Items != nil && len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		if item.Price != nil {
			p.StripePriceID = item.Price.ID
			if item.Price.Product != nil {
				p.StripeProductID = item.Price.Product.ID
			}
		}
	}
	if sub.TrialEnd != 0 {
		t := time.Unix(sub.TrialEnd, 0)
		p.TrialEndsAt = &t
	}
	// In v82, CurrentPeriodStart/End live on each SubscriptionItem.
	if sub.Items != nil && len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		if item.CurrentPeriodStart != 0 {
			t := time.Unix(item.CurrentPeriodStart, 0)
			p.CurrentPeriodStart = &t
		}
		if item.CurrentPeriodEnd != 0 {
			t := time.Unix(item.CurrentPeriodEnd, 0)
			p.CurrentPeriodEnd = &t
		}
	}
	if sub.CanceledAt != 0 {
		t := time.Unix(sub.CanceledAt, 0)
		p.CanceledAt = &t
	}

	return c.repo.upsertSubscription(ctx, p)
}

func (c *Client) onSubscriptionDeleted(ctx context.Context, sub *stripe.Subscription) error {
	existing, err := c.repo.getSubscriptionByCustomerID(ctx, sub.Customer.ID)
	if err != nil {
		return nil // already unknown
	}
	now := time.Now()
	return c.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               existing.UserID,
		StripeCustomerID:     sub.Customer.ID,
		StripeSubscriptionID: sub.ID,
		Status:               StatusCanceled,
		CanceledAt:           &now,
	})
}

func (c *Client) onInvoicePaymentSucceeded(ctx context.Context, inv *stripe.Invoice) error {
	if inv.Customer == nil {
		return nil
	}
	existing, err := c.repo.getSubscriptionByCustomerID(ctx, inv.Customer.ID)
	if err != nil {
		return nil // not our user
	}

	// In v82, subscription reference is nested under Parent.SubscriptionDetails.
	var subID string
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil && inv.Parent.SubscriptionDetails.Subscription != nil {
		subID = inv.Parent.SubscriptionDetails.Subscription.ID
	}

	p := upsertSubParams{
		UserID:               existing.UserID,
		StripeCustomerID:     inv.Customer.ID,
		StripeSubscriptionID: subID,
		Status:               StatusActive,
	}
	if inv.PeriodStart != 0 {
		t := time.Unix(inv.PeriodStart, 0)
		p.CurrentPeriodStart = &t
	}
	if inv.PeriodEnd != 0 {
		t := time.Unix(inv.PeriodEnd, 0)
		p.CurrentPeriodEnd = &t
	}
	return c.repo.upsertSubscription(ctx, p)
}

func (c *Client) onInvoicePaymentFailed(ctx context.Context, inv *stripe.Invoice) error {
	if inv.Customer == nil {
		return nil
	}
	existing, err := c.repo.getSubscriptionByCustomerID(ctx, inv.Customer.ID)
	if err != nil {
		return nil
	}

	var subID string
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil && inv.Parent.SubscriptionDetails.Subscription != nil {
		subID = inv.Parent.SubscriptionDetails.Subscription.ID
	}
	return c.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               existing.UserID,
		StripeCustomerID:     inv.Customer.ID,
		StripeSubscriptionID: subID,
		Status:               StatusPastDue,
	})
}

func (c *Client) onProductUpserted(ctx context.Context, prod *stripe.Product) error {
	var createdAt *time.Time
	if prod.Created != 0 {
		t := time.Unix(prod.Created, 0)
		createdAt = &t
	}
	return c.repo.upsertProduct(ctx, Product{
		ID:              prod.ID,
		Name:            prod.Name,
		Description:     prod.Description,
		Active:          prod.Active,
		StripeCreatedAt: createdAt,
	})
}

func (c *Client) onPriceUpserted(ctx context.Context, p *stripe.Price) error {
	if p.Product == nil {
		return nil
	}
	var createdAt *time.Time
	if p.Created != 0 {
		t := time.Unix(p.Created, 0)
		createdAt = &t
	}

	lp := Price{
		ID:              p.ID,
		ProductID:       p.Product.ID,
		Currency:        string(p.Currency),
		Active:          p.Active,
		StripeCreatedAt: createdAt,
	}
	if p.UnitAmount != 0 {
		ua := p.UnitAmount
		lp.UnitAmount = &ua
	}
	if p.Recurring != nil {
		lp.RecurringInterval = string(p.Recurring.Interval)
		count := int(p.Recurring.IntervalCount)
		lp.RecurringCount = &count
	}
	return c.repo.upsertPrice(ctx, lp)
}
