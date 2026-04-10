package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"propcopyai/pkg/config"
	"propcopyai/pkg/db/models"
	"propcopyai/pkg/toast"
	"strconv"
	"time"

	"github.com/josuebrunel/gopkg/etr"

	"github.com/aarondl/opt/omit"
	"github.com/aarondl/opt/omitnull"
	"github.com/josuebrunel/ezauth"
	"github.com/josuebrunel/gopkg/xlog"
	"github.com/labstack/echo/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stripe/stripe-go/v82"
	portalSession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/checkout/session"
	stSubscription "github.com/stripe/stripe-go/v82/subscription"
	"github.com/stripe/stripe-go/v82/webhook"
)

const (
	// Handlers
	HandlerNamePricing  = "subscription.pricing"
	HandlerNameCheckout = "subscription.checkout"
	HandlerNamePortal   = "subscription.portal"
	HandlerNameWebhook  = "subscription.webhook"
	HandlerNameUserSub  = "subscription.user"
	// Statuses
	StatusActive    = "active"
	StatusCanceled  = "canceled"
	StatusTrialing  = "trialing"
	CheckoutSuccess = "success"
	CheckoutFailed  = "failed"
	// Metadas
	MetadataUserID           = "user_id"
	MetadataUserEmail        = "user_email"
	MetadataPlanID           = "plan_id"
	MetadataPlanName         = "plan_name"
	MetadataPlanPrice        = "plan_price"
	MetadataPlanMaxDesc      = "plan_max_desc"
	MetadataStripePriceID    = "stripe_price_id"
	MetadataStripeProductID  = "stripe_product_id"
	MetadataStripeCustomerID = "stripe_customer_id"
	BillingCycleMonthly      = "monthly"
	BillingCycleYearly       = "yearly"
)

type Handler struct {
	name string
	bdb  bob.DB
	auth *ezauth.EzAuth
}

func New(db *sql.DB, auth *ezauth.EzAuth) Handler {
	return Handler{
		name: HandlerNamePricing,
		bdb:  bob.NewDB(db),
		auth: auth,
	}
}

func Mount(ctx context.Context, e *echo.Echo, db *sql.DB, auth *ezauth.EzAuth) {
	h := New(db, auth)
	g := e.Group("/subscription")
	g.AddRoute(echo.Route{Method: http.MethodGet, Path: "/pricing", Handler: h.GetPricing(ctx), Name: HandlerNamePricing})
	g.AddRoute(echo.Route{Method: http.MethodPost, Path: "/checkout", Handler: h.Checkout(ctx), Name: HandlerNameCheckout,
		Middlewares: []echo.MiddlewareFunc{echo.WrapMiddleware(auth.LoginRequiredMiddleware)}})
	g.AddRoute(echo.Route{Method: http.MethodGet, Path: "/portal", Handler: h.Portal(ctx), Name: HandlerNamePortal,
		Middlewares: []echo.MiddlewareFunc{echo.WrapMiddleware(auth.LoginRequiredMiddleware)}})
	g.AddRoute(echo.Route{Method: http.MethodPost, Path: "/webhook", Handler: h.Webhook(ctx), Name: HandlerNameWebhook})
}

func (h *Handler) GetPricing(ctx context.Context) echo.HandlerFunc {
	return func(c *echo.Context) error {
		plans, err := getPlans(c.Request().Context(), h.bdb)
		if err != nil {
			xlog.Error("Error querying plans", "error", err)
			return etr.Render(c, http.StatusOK, PlansView(plans), nil)
		}
		return etr.Render(c, http.StatusOK, PlansView(plans), nil)
	}
}

func (h *Handler) Checkout(ctx context.Context) echo.HandlerFunc {
	return func(c *echo.Context) error {
		user, _ := h.auth.GetSessionUser(c.Request().Context())
		planID := c.FormValue("plan_id")
		plan, err := models.FindPlan(c.Request().Context(), h.bdb, planID)
		if err != nil {
			xlog.Error("Error finding plan", "error", err)
		}
		metadata := map[string]string{
			MetadataUserID:          user.ID,
			MetadataUserEmail:       user.Email,
			MetadataPlanID:          planID,
			MetadataPlanName:        plan.Name,
			MetadataPlanPrice:       strconv.Itoa(int(plan.PriceUsd)),
			MetadataPlanMaxDesc:     strconv.Itoa(int(plan.MaxDescriptions.GetOrZero())),
			MetadataStripePriceID:   plan.StripePriceID,
			MetadataStripeProductID: plan.StripeProductID,
		}
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
			SuccessURL:    stripe.String(checkoutRedirectURL(CheckoutSuccess)),
			CancelURL:     stripe.String(checkoutRedirectURL(CheckoutFailed)),
			Metadata:      metadata,
			SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
				Metadata: metadata,
			},
		}
		stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
		sess, err := session.New(params)
		if err != nil {
			xlog.Error("Error creating checkout session", "error", err)
			return c.Redirect(http.StatusSeeOther, "/")
		}
		return c.Redirect(http.StatusSeeOther, sess.URL)
	}
}

func (h *Handler) Portal(ctx context.Context) echo.HandlerFunc {
	return func(c *echo.Context) error {
		user, _ := h.auth.GetSessionUser(c.Request().Context())
		sub, err := models.Subscriptions.Query(
			sm.Where(psql.Quote("user_id").EQ(psql.Arg(user.ID))),
			sm.OrderBy("date_start DESC"),
			sm.Limit(1),
		).One(c.Request().Context(), h.bdb)
		if err != nil {
			xlog.Error("Error finding subscription", "user", user.ID, "error", err)
			toast.NotifyError(c, "No active subscription found.")
			return c.Redirect(http.StatusSeeOther, "/account/?action=subscription.pricing")
		}
		stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
		params := &stripe.BillingPortalSessionParams{
			Customer:  stripe.String(sub.StripeCustomerID),
			ReturnURL: stripe.String(checkoutRedirectURL(CheckoutSuccess)),
		}
		sess, err := portalSession.New(params)
		if err != nil {
			xlog.Error("Error creating billing portal session", "error", err)
			return c.Redirect(http.StatusSeeOther, "/account/")
		}
		return c.Redirect(http.StatusSeeOther, sess.URL)
	}

}

func (h *Handler) Webhook(ctx context.Context) echo.HandlerFunc {
	return func(c *echo.Context) error {
		xlog.Info("Received webhook", "host", c.Request().Host)
		var (
			ctx   = c.Request().Context()
			event stripe.Event
		)

		payload, err := io.ReadAll(c.Request().Body)
		if err != nil {
			xlog.Error("Error reading request body", "error", err)
			return c.NoContent(http.StatusInternalServerError)
		}
		event, err = webhook.ConstructEventWithOptions(
			payload, c.Request().Header.Get("Stripe-Signature"), os.Getenv("STRIPE_WEBHOOK_SECRET"),
			webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
		)
		if err != nil {
			xlog.Error("Error binding event", "error", err)
			return c.NoContent(http.StatusInternalServerError)
		}
		xlog.Debug("Received webhook event", "type", event.Type, "data", event.Data)
		switch event.Type {
		case stripe.EventTypeCustomerSubscriptionUpdated:
			var subscription stripe.Subscription
			_ = json.Unmarshal(event.Data.Raw, &subscription)
			xlog.Debug("Subscription updated", "subscription", subscription)

			xlog.Info("Subscription updated", "subscription", event.Data.Object["id"], "customer", event.Data.Object["customer"])
		case stripe.EventTypeCustomerSubscriptionDeleted:
			var subscription stripe.Subscription
			_ = json.Unmarshal(event.Data.Raw, &subscription)

			sub, err := models.Subscriptions.Query(
				sm.Where(psql.Raw("stripe_subscription_id = ? AND stripe_customer_id = ?", subscription.ID, subscription.Customer.ID)),
			).One(ctx, h.bdb)
			if err != nil {
				xlog.Error("Error finding subscription", "error", err)
				return c.NoContent(http.StatusOK)
			}

			err = sub.Delete(ctx, h.bdb)
			if err != nil {
				xlog.Error("Error deleting subscription", "error", err)
				return c.NoContent(http.StatusOK)
			}

			xlog.Info("Subscription deleted", "subscription", event.Data.Object["id"], "customer", event.Data.Object["customer"])
		case stripe.EventTypeInvoicePaid:
			var invoice stripe.Invoice
			if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
				xlog.Error("Error unmarshaling invoice", "error", err)
				return c.NoContent(http.StatusInternalServerError)
			}
			xlog.Info("Invoice paid", "invoice", invoice)
			stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
			stSubID := invoice.Parent.SubscriptionDetails.Subscription.ID
			stSub, err := stSubscription.Get(stSubID, nil)
			if err != nil {
				xlog.Error("Error getting subscription", "error", err)
				return c.NoContent(http.StatusOK)
			}

			if len(stSub.Items.Data) == 0 {
				xlog.Error("No items in stSubscription", "stSubscription", stSub)
				return c.NoContent(http.StatusOK)
			}

			stItem := stSub.Items.Data[len(stSub.Items.Data)-1]
			var (
				stPrice   = stItem.Price
				dateStart = stItem.CurrentPeriodStart
				dateEnd   = stItem.CurrentPeriodEnd
			)

			plan, err := models.FindPlan(ctx, h.bdb, stPrice.ID)
			if err != nil {
				xlog.Error("Error finding plan", "error", err)
				return c.NoContent(http.StatusInternalServerError)
			}
			userID, err := getUserByEmail(ctx, h.bdb, invoice.CustomerEmail)
			if err != nil {
				xlog.Error("Error finding user", "error", err)
				return c.NoContent(http.StatusInternalServerError)
			}

			query := models.Subscriptions.Insert(&models.SubscriptionSetter{
				StripeCustomerID:     omit.From(invoice.Customer.ID),
				StripePriceID:        omit.From(stPrice.ID),
				StripeSubscriptionID: omit.From(stSubID),
				UserID:               omit.From(userID),
				PlanName:             omitnull.From(plan.Name),
				DateEnd:              omitnull.From(time.Unix(dateEnd, 0).UTC()),
				DateStart:            omitnull.From(time.Unix(dateStart, 0).UTC()),
				DateRenewal:          omitnull.From(time.Unix(dateEnd, 0).UTC()),
				Status:               omitnull.From(StatusActive),
				UsageDesc:            omitnull.From(plan.MaxDescriptions.GetOrZero()),
				UsagePhotos:          omitnull.From(plan.MaxPhotos.GetOrZero()),
			},
				im.OnConflict("stripe_customer_id", "stripe_subscription_id").DoUpdate(
					im.SetExcluded("plan_name"),
					im.SetExcluded("stripe_price_id"),
					im.SetExcluded("date_end"),
					im.SetExcluded("date_start"),
					im.SetExcluded("date_renewal"),
					im.SetExcluded("status"),
					im.SetExcluded("stripe_subscription_id"),
					im.SetExcluded("usage_desc"),
					im.SetExcluded("usage_photos"),
				),
			)
			xlog.Debug("query", "query", query)
			sub, err := query.One(ctx, h.bdb)
			if err != nil {
				xlog.Error("Error inserting user subscription", "email", invoice.CustomerEmail, "subscription", stSubID, "error", err)
				return c.NoContent(http.StatusOK)
			}
			xlog.Info("User subscription created", "subscription", sub)

		case stripe.EventTypeInvoicePaymentFailed:
			xlog.Info("Invoice payment failed", "invoice", event.Data.Object)
		default:
			xlog.Info("Unhandled event", "event", event.Type)
		}
		return c.NoContent(http.StatusOK)
	}
}

func checkoutRedirectURL(mode string) string {
	redirectURL := config.Cfg.Stripe.CheckoutRedirect
	u, err := url.Parse(redirectURL)
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
