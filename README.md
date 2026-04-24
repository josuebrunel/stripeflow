# StripeFlow

StripeFlow is a pluggable Go library that focuses exclusively on integrating the Stripe Customer Portal into any Go application. It handles billing portal generation, webhook processing, subscription state, product/price catalogue syncing, and built-in usage tracking — with support for **PostgreSQL**, **MySQL**, and **SQLite**.

## Features

- **Programmatic Portal** — `CreatePortalSession()` returns a URL; you control the redirect
- **Webhook processing** — handles the full subscription lifecycle with **idempotency guarantees**
- **Product & Price sync** — sync Stripe catalogue to a local database on startup or via cron
- **Subscription middleware** — protect routes requiring an active or trialing subscription
- **Built-in usage tracking** — per-user `usage_count` / `usage_limit` with atomic increment
- **Typed error sentinels** — `ErrNoSubscription`, `ErrTrialExpired`, `ErrUsageLimitReached`, etc.
- **Multi-dialect** — works with PostgreSQL, MySQL, and SQLite
- **Zero framework dependency** — uses only `net/http`, `log/slog`, and `database/sql`

## Installation

```sh
go get github.com/josuebrunel/stripeflow
```

## Quick Start

### 1. Run Migrations

StripeFlow uses embedded Goose migrations to create and version all required tables.

```go
import (
    "database/sql"
    _ "github.com/lib/pq"
    "github.com/josuebrunel/stripeflow/migrations"
)

db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))

if err := migrations.MigrateUp(db, "postgres"); err != nil {
    log.Fatalf("migration failed: %v", err)
}
```

Supported dialect values: `"postgres"`, `"mysql"`, `"sqlite"`.

### 2. Initialise the Client

```go
import (
    "net/http"
    "github.com/josuebrunel/stripeflow"
)

sf, err := stripeflow.New(stripeflow.Config{
    Dialect:         stripeflow.Postgres,      // stripeflow.Postgres | stripeflow.MySQL | stripeflow.SQLite
    DB:              db,
    StripeSecretKey: os.Getenv("STRIPE_SECRET_KEY"),
    WebhookSecret:   os.Getenv("STRIPE_WEBHOOK_SECRET"),
    TrialDays:       14,                       // global trial default (0 = no trial)
    UsageLimitEnabled: true,                   // enforce usage_limit in middleware

    // Tell middleware how to identify the current user.
    GetUserID: func(r *http.Request) (string, error) {
        return sessionUserID(r), nil           // parse JWT, cookie, etc.
    },

    // Optional: called after every successfully processed webhook event.
    OnEvent: func(event *stripe.Event) {
        log.Printf("stripe event: %s", event.Type)
    },
})
if err != nil {
    log.Fatal(err)
}
```

### 3. Sync Products & Register Webhook

```go
ctx := context.Background()

// Sync Stripe product catalogue to the local database.
result, err := sf.SyncProducts(ctx)
// result.ProductsUpserted, result.PricesUpserted

mux := http.NewServeMux()

// Webhook endpoint (mount at the URL configured in your Stripe dashboard).
mux.Handle("POST /stripe/webhook", sf.WebhookHandler())

log.Fatal(http.ListenAndServe(":8080", mux))
```

---

## Checkout & Billing Portal

Both APIs are **programmatic** — they return a URL and you redirect the user.

### Open a Checkout Session

```go
url, err := sf.CreateCheckoutSession(ctx, stripeflow.CheckoutParams{
    UserID:     currentUserID,
    PriceID:    "price_XYZ123",
    SuccessURL: "https://myapp.com/success",
    CancelURL:  "https://myapp.com/cancel",
})
if err != nil {
    http.Error(w, err.Error(), http.StatusInternalServerError)
    return
}
http.Redirect(w, r, url, http.StatusSeeOther)
```

### Open the Billing Portal

```go
url, err := sf.CreatePortalSession(ctx, stripeflow.PortalParams{
    UserID:    currentUserID,
    ReturnURL: "https://myapp.com/account",
})
if err != nil {
    http.Error(w, err.Error(), http.StatusInternalServerError)
    return
}
http.Redirect(w, r, url, http.StatusSeeOther)
```

---

## Middleware

Middleware injects the resolved `*Subscription` into the request context and is accessible via `SubscriptionFromContext`.

### Protect Routes

```go
// Allow active subscribers AND users in a valid trial.
mux.Handle("/app/", sf.RequireActiveOrTrial(appHandler))

// Require a fully paid subscription (no trials).
mux.Handle("/api/premium", sf.RequireActiveSubscription(premiumHandler))

// Full control via MiddlewareOptions.
mux.Handle("/api/", sf.RequireSubscription(apiHandler, stripeflow.MiddlewareOptions{
    AllowTrialing:   false,
    CheckUsageLimit: true,  // deny when usage_count >= usage_limit
    OnDenied: func(w http.ResponseWriter, r *http.Request, reason error) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusPaymentRequired)
        json.NewEncoder(w).Encode(map[string]string{
            "error":       "upgrade_required",
            "upgrade_url": "https://myapp.com/pricing",
        })
    },
}))
```

### Read Subscription in Handlers

```go
func appHandler(w http.ResponseWriter, r *http.Request) {
    sub, ok := stripeflow.SubscriptionFromContext(r.Context())
    if ok {
        fmt.Fprintf(w, "plan: %s, usage: %d/%v", sub.StripePriceID, sub.UsageCount, sub.UsageLimit)
    }
}
```

### Default Denial Responses

When `OnDenied` is not set, the middleware returns structured JSON with HTTP status codes mapped to sentinel errors:

| Error | HTTP Status | JSON `error` key |
|---|---|---|
| `ErrNoSubscription` | `402` | `no_subscription` |
| `ErrTrialExpired` | `402` | `trial_expired` |
| `ErrSubscriptionInactive` | `402` | `subscription_inactive` |
| `ErrUsageLimitReached` | `429` | `usage_limit_reached` |

---

## Usage Tracking

stripeflow stores a `usage_count` and optional `usage_limit` directly on the subscription row.

```go
// Increment usage after a successful operation.
newCount, err := sf.IncrementUsage(ctx, userID, 1)

// Set a cap (nil = unlimited).
err = sf.SetUsageLimit(ctx, userID, stripeflow.Int64Ptr(1000))

// Reset at the start of a billing period (e.g. via OnEvent hook).
err = sf.ResetUsage(ctx, userID)
```

---

## Products & Prices

### Sync from Stripe

```go
result, err := sf.SyncProducts(ctx)
// Fetches all products + prices from Stripe and upserts them locally.
```

### List Locally Cached Catalogue

```go
products, err := sf.ListProducts(ctx, true /* activeOnly */)
prices, err   := sf.ListPrices(ctx, "prod_ABC123")
```

### Create Programmatically

```go
product, err := sf.CreateProduct(ctx, stripeflow.CreateProductParams{
    Name:        "Pro Plan",
    Description: "All features, unlimited usage",
})

price, err := sf.CreatePrice(ctx, stripeflow.CreatePriceParams{
    StripeProductID: product.ID,
    UnitAmount:      1999, // $19.99
    Currency:        "usd",
    Recurring: &stripeflow.RecurringParams{
        Interval:      stripeflow.IntervalMonth,
        IntervalCount: 1,
    },
})
```

### Provision Products from JSON

Use `ProvisionProduct` or `ProvisionProductsFromJSON` to create your products and all their prices in a single call. This is ideal for CLI tools, seed scripts, or any workflow where you define your catalogue as JSON.

**Programmatic usage:**

```go
result, err := sf.ProvisionProduct(ctx, stripeflow.ProvisionParams{
    Product: stripeflow.ProvisionProductParams{
        Name:        "My SaaS",
        Description: "AI-powered analytics platform",
        MarketingFeatures: []stripeflow.ProvisionFeature{
            {Name: "Real-time dashboards"},
            {Name: "Unlimited team members"},
        },
        Metadata: map[string]string{"category": "analytics"},
    },
    Prices: []stripeflow.ProvisionPriceParams{
        {
            Nickname:      "Starter — monthly",
            Currency:       "usd",
            BillingScheme: "per_unit",
            UnitAmount:    2990,
            Recurring:     &stripeflow.ProvisionRecurringParams{Interval: "month"},
        },
        {
            Nickname:      "Starter — annual (20% off)",
            Currency:       "usd",
            BillingScheme: "per_unit",
            UnitAmount:    28704,
            Recurring:     &stripeflow.ProvisionRecurringParams{Interval: "year"},
        },
    },
})
// result.ProductID  → "prod_ABC123"
// result.Prices[0].PriceID → "price_XYZ001"
```

**From a JSON file (e.g. in a CLI tool):**

```go
raw, err := os.ReadFile("products.json")
if err != nil {
    log.Fatal(err)
}
results, err := sf.ProvisionProductsFromJSON(ctx, raw)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Created %d products\n", len(results))
```

**Example `products.json`:**

```json
[
  {
    "product": {
      "name": "My SaaS",
      "description": "AI-powered analytics platform",
      "metadata": { "category": "analytics" },
      "marketing_features": [
        { "name": "Real-time dashboards" },
        { "name": "Unlimited team members" }
      ]
    },
    "prices": [
      {
        "nickname": "Starter — monthly",
        "currency": "usd",
        "billing_scheme": "per_unit",
        "unit_amount": 2990,
        "recurring": {
          "interval": "month",
          "usage_type": "licensed"
        }
      },
      {
        "nickname": "Metered API calls",
        "currency": "usd",
        "billing_scheme": "per_unit",
        "unit_amount": 1,
        "recurring": {
          "interval": "month",
          "usage_type": "metered",
          "meter_event_name": "api_calls"
        },
        "transform_quantity": {
          "divide_by": 1000,
          "round": "up"
        }
      }
    ]
  }
]
```

---

## Webhooks

Mount `WebhookHandler()` and configure the same URL in your Stripe dashboard.

```go
mux.Handle("POST /stripe/webhook", sf.WebhookHandler())
```

**Handled events:**

| Event | Action |
|---|---|
| `customer.subscription.created/updated` | Updates subscription status & period |
| `customer.subscription.deleted` | Marks subscription as `canceled` |
| `customer.subscription.trial_will_end` | Informational — fire via `OnEvent` for emails |
| `invoice.payment_succeeded` | Marks subscription `active`, updates period |
| `invoice.payment_failed` | Marks subscription `past_due` |
| `product.created/updated/deleted` | Upserts local product |
| `price.created/updated/deleted` | Upserts local price |

All events are idempotent — duplicate deliveries are safely ignored via the `stripeflow_webhook_events` table.

Use `OnEvent` for side-effects like cache invalidation or sending emails:

```go
stripeflow.Config{
    OnEvent: func(event *stripe.Event) {
        if event.Type == "customer.subscription.trial_will_end" {
            sendTrialEndingEmail(event)
        }
    },
}
```

---

## API Reference

### Client Methods

| Method | Description |
|---|---|
| `CreateCheckoutSession(ctx, CheckoutParams) (string, error)` | Create a Stripe Checkout session |
| `CreatePortalSession(ctx, PortalParams) (string, error)` | Create a Stripe Billing Portal session |
| `WebhookHandler() http.Handler` | Verified webhook event handler |
| `Handler() http.Handler` | Thin convenience mux (checkout + portal + webhook) |
| `RequireSubscription(next, ...opts) http.Handler` | Subscription-required middleware |
| `RequireActiveOrTrial(next) http.Handler` | Allows active + trialing users |
| `RequireActiveSubscription(next) http.Handler` | Paid subscription only (no trials) |
| `GetSubscription(ctx, userID) (*Subscription, error)` | Fetch subscription state |
| `GetSubscriptionByID(ctx, id)` | Fetch subscription by ID |
| `GetSubscriptionByCustomerID(ctx, customerID)` | Fetch subscription by Stripe customer ID |
| `GetSubscriptionByStripeSubID(ctx, subID)` | Fetch subscription by Stripe subscription ID |
| `GetProductByID(ctx, id)` | Fetch product by ID |
| `IncrementUsage(ctx, userID, delta) (int64, error)` | Atomically increment usage counter |
| `SetUsageLimit(ctx, userID, *int64) error` | Set or remove usage cap |
| `ResetUsage(ctx, userID) error` | Zero usage counter |
| `SyncProducts(ctx) (*SyncResult, error)` | Pull Stripe catalogue → local DB |
| `ListProducts(ctx, activeOnly) ([]Product, error)` | List local products |
| `ListPrices(ctx, productID) ([]Price, error)` | List local prices for a product |
| `CreateProduct(ctx, CreateProductParams) (*Product, error)` | Create product in Stripe + local |
| `UpdateProduct(ctx, UpdateProductParams) (*Product, error)` | Update product in Stripe + local |
| `CreatePrice(ctx, CreatePriceParams) (*Price, error)` | Create price in Stripe + local |
| `ArchivePrice(ctx, priceID) error` | Archive price in Stripe |
| `DeleteProduct(ctx, productID) error` | Delete a product and archive its prices in Stripe, and remove locally |
| `DeleteAllProducts(ctx) error` | Delete all products and prices, removing them locally and from Stripe |
| `ProvisionProduct(ctx, ProvisionParams) (*ProvisionResult, error)` | Create product + all prices in one call |
| `ProvisionProductsFromJSON(ctx, []byte) ([]ProvisionResult, error)` | Provision array of products from JSON |

### Helpers

```go
stripeflow.SubscriptionFromContext(ctx) (*Subscription, bool)
stripeflow.Int64Ptr(v int64) *int64
```

---

## Database Tables

| Table | Purpose |
|---|---|
| `stripeflow_products` | Stripe products synced locally |
| `stripeflow_prices` | Stripe prices synced locally |
| `stripeflow_subscriptions` | One row per user — subscription state + usage |
| `stripeflow_webhook_events` | Idempotency log of processed Stripe events |

---

## Running Tests

```sh
# Unit tests (SQLite in-memory, no external services)
go test -v ./...

# Integration tests (requires Docker)
go test -v -run TestPostgresAndMySQLIntegration ./...
```

---

## CLI Tool

StripeFlow comes with a built-in CLI tool to help you manage your database migrations, products, and syncing.

### Installation

```sh
go run github.com/josuebrunel/stripeflow/cmd/stripeflow@latest
```

### Configuration

The CLI uses the following environment variables. Note that the variables are prefixed with `STRIPEFLOW_`.

```sh
export STRIPEFLOW_DATABASE_URL="postgres://user:pass@localhost:5432/dbname?sslmode=disable"
export STRIPEFLOW_STRIPE_SECRET_KEY="sk_test_..."
export STRIPEFLOW_WEBHOOK_SECRET="whsec_..."
```

*(Note: SQLite (`sqlite://...`) and MySQL (`mysql://...`) URLs are also supported).*

### Usage

**Run Database Migrations:**

```sh
stripeflow -migrate=up
stripeflow -migrate=down
```

**Sync Products from Stripe:**

Fetches all products and their prices from your Stripe account and upserts them locally.

```sh
stripeflow -sync
```

**Provision Products from JSON:**

Creates one or more products and their prices in Stripe, and syncs them to your local database.

```sh
stripeflow -provision=products.json
```

**Delete a Product or All Products:**

```sh
stripeflow -delete="prod_12345" # Deletes a single product and archives its prices
stripeflow -delete="all"        # Deletes all products and prices locally and from Stripe
```
