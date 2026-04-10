package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"stripeflow/repository"
	"stripeflow/service"

	"github.com/go-chi/chi/v5"
	"github.com/josuebrunel/gopkg/xlog"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
	portalSession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/webhook"
)

const (
	StatusActive    = "active"
	StatusCanceled  = "canceled"
	StatusTrialing  = "trialing"
	CheckoutSuccess = "success"
	CheckoutFailed  = "failed"
	// Metadata
	MetadataUserID           = "user_id"
	MetadataUserEmail        = "user_email"
	MetadataPlanID           = "plan_id"
	MetadataPlanName         = "plan_name"
	MetadataPlanPrice        = "plan_price"
	MetadataPlanMaxDesc      = "plan_max_desc"
	MetadataStripePriceID    = "stripe_price_id"
	MetadataStripeProductID  = "stripe_product_id"
	MetadataStripeCustomerID = "stripe_customer_id"
)

type Handler struct {
	repo         repository.Querier
	svc          *service.Service
	cfg          service.Config
	userResolver UserResolver
}

type User struct {
	ID    string
	Email string
}

type UserDetailsResolver interface {
	UserResolver
	GetUserDetails(ctx context.Context) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (string, error)
}

func New(repo repository.Querier, svc *service.Service, cfg service.Config, userResolver UserDetailsResolver) *Handler {
	return &Handler{
		repo:         repo,
		svc:          svc,
		cfg:          cfg,
		userResolver: userResolver,
	}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/checkout", h.Checkout)
	r.Get("/portal", h.Portal)
	r.Post("/webhook", h.Webhook)
}

func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	resolver, ok := h.userResolver.(UserDetailsResolver)
	if !ok {
		xlog.Error("User resolver does not support getting user details")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := resolver.GetUserDetails(r.Context())
	if err != nil || user == nil {
		xlog.Error("Error getting user", "error", err)
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

	plan, err := h.repo.FindPlan(r.Context(), planID)
	if err != nil || plan == nil {
		xlog.Error("Error finding plan", "error", err)
		http.Error(w, "Plan not found", http.StatusNotFound)
		return
	}

	metadata := map[string]string{
		MetadataUserID:          user.ID,
		MetadataUserEmail:       user.Email,
		MetadataPlanID:          planID,
		MetadataPlanName:        plan.Name,
		MetadataPlanPrice:       strconv.Itoa(int(plan.PriceUsd)),
		MetadataPlanMaxDesc:     strconv.Itoa(int(plan.MaxDescriptions)),
		MetadataStripePriceID:   plan.StripePriceID,
		MetadataStripeProductID: plan.StripeProductID,
	}

	stripe.Key = h.cfg.StripeSecretKey
	params := &stripe.CheckoutSessionParams{
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(plan.StripePriceID),
				Quantity: stripe.Int64(1),
			},
		},
		Mode:          stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		AutomaticTax:  &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
		CustomerEmail: stripe.String(user.Email),
		SuccessURL:    stripe.String(h.checkoutRedirectURL(CheckoutSuccess)),
		CancelURL:     stripe.String(h.checkoutRedirectURL(CheckoutFailed)),
		Metadata:      metadata,
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: metadata,
		},
	}

	sess, err := session.New(params)
	if err != nil {
		xlog.Error("Error creating checkout session", "error", err)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, sess.URL, http.StatusSeeOther)
}

func (h *Handler) Portal(w http.ResponseWriter, r *http.Request) {
	resolver, ok := h.userResolver.(UserDetailsResolver)
	if !ok {
		xlog.Error("User resolver does not support getting user details")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := resolver.GetUserDetails(r.Context())
	if err != nil || user == nil {
		xlog.Error("Error getting user", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	sub, err := h.repo.FindSubscriptionByUserID(r.Context(), user.ID)
	if err != nil || sub == nil {
		xlog.Error("Error finding subscription", "user", user.ID, "error", err)
		http.Redirect(w, r, "/account/?action=subscription.pricing", http.StatusSeeOther)
		return
	}

	stripe.Key = h.cfg.StripeSecretKey
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sub.StripeCustomerID),
		ReturnURL: stripe.String(h.checkoutRedirectURL(CheckoutSuccess)),
	}
	sess, err := portalSession.New(params)
	if err != nil {
		xlog.Error("Error creating billing portal session", "error", err)
		http.Redirect(w, r, "/account/", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, sess.URL, http.StatusSeeOther)
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	xlog.Info("Received webhook", "host", r.Host)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		xlog.Error("Error reading request body", "error", err)
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}

	event, err := webhook.ConstructEventWithOptions(
		payload, r.Header.Get("Stripe-Signature"), h.cfg.StripeWebhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
	)
	if err != nil {
		xlog.Error("Error binding event", "error", err)
		http.Error(w, "Error binding event", http.StatusBadRequest)
		return
	}

	xlog.Debug("Received webhook event", "type", event.Type, "data", event.Data)

	switch event.Type {
	case stripe.EventTypeCustomerSubscriptionUpdated:
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			xlog.Error("Error unmarshaling subscription", "error", err)
			break
		}
		xlog.Info("Subscription updated", "subscription", subscription.ID, "customer", subscription.Customer.ID)
	case stripe.EventTypeCustomerSubscriptionDeleted:
		var subscription stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
			xlog.Error("Error unmarshaling subscription", "error", err)
			break
		}
		err = h.svc.HandleSubscriptionDeleted(r.Context(), &subscription)
		if err != nil {
			xlog.Error("Error handling subscription deleted", "error", err)
		}
	case stripe.EventTypeInvoicePaid:
		var invoice stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			xlog.Error("Error unmarshaling invoice", "error", err)
			http.Error(w, "Error processing event", http.StatusInternalServerError)
			return
		}
		xlog.Info("Invoice paid", "invoice", invoice.ID)

		resolver, ok := h.userResolver.(UserDetailsResolver)
		if !ok {
			xlog.Error("User resolver does not support getting user details")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, err := resolver.GetUserByEmail(r.Context(), invoice.CustomerEmail)
		if err != nil {
			xlog.Error("Error finding user by email", "email", invoice.CustomerEmail, "error", err)
			w.WriteHeader(http.StatusOK)
			return
		}

		err = h.svc.HandleInvoicePaid(r.Context(), &invoice, userID)
		if err != nil {
			xlog.Error("Error handling invoice paid", "error", err)
		}
	case stripe.EventTypeInvoicePaymentFailed:
		xlog.Info("Invoice payment failed", "invoice", event.Data.Object)
	default:
		xlog.Info("Unhandled event", "event", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) checkoutRedirectURL(mode string) string {
	u, err := url.Parse(h.cfg.CheckoutRedirect)
	if err != nil {
		return ""
	}
	q := u.Query()
	if mode == "success" {
		q.Set("checkout", CheckoutSuccess)
	} else {
		q.Set("checkout", CheckoutFailed)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
