# StripeFlow

StripeFlow is a pluggable Go library for integrating Stripe subscriptions into your application. It handles checkout sessions, billing portal, webhook processing, and subscription state — with support for PostgreSQL, MySQL, and SQLite.

## Features

- **Checkout & Portal** — pre-built HTTP handlers for Stripe Checkout and Billing Portal
- **Webhook processing** — handles subscription lifecycle events (`invoice.paid`, `customer.subscription.updated/deleted`)
- **Plan syncing** — syncs Stripe prices to a local database
- **Subscription middleware** — protect routes by requiring an active subscription
- **Multi-dialect** — works with PostgreSQL, MySQL, and SQLite
- **Zero framework dependency** — uses stdlib `net/http` and `log/slog`

## Installation

```sh
go get stripeflow
```

## Quick Start

### 1. Run Migrations

StripeFlow uses embedded Goose migrations to create the required tables.

```go
import (
    "database/sql"
    "log"

    _ "github.com/lib/pq"
    "stripeflow/migrations"
)

func main() {
    db, err := sql.Open("postgres", "postgres://user:pass@localhost:5432/dbname?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    if err := migrations.MigrateUp(db, "postgres"); err != nil {
        log.Fatalf("migration failed: %v", err)
    }
}
```

### 2. Implement UserResolver

StripeFlow needs a way to identify users in your application:

```go
import (
    "context"
    "stripeflow"
)

type MyUserResolver struct {
    // your auth/session dependencies
}

func (r *MyUserResolver) GetUserID(ctx context.Context) (string, error) {
    return "user-id-from-session", nil
}

func (r *MyUserResolver) GetUserEmail(ctx context.Context) (string, error) {
    return "user@example.com", nil
}

func (r *MyUserResolver) FindUserIDByEmail(ctx context.Context, email string) (string, error) {
    return "user-id", nil
}
```

### 3. Initialize & Mount

```go
import (
    "context"
    "database/sql"
    "log"
    "net/http"

    _ "github.com/lib/pq"
    "stripeflow"
)

func main() {
    db, _ := sql.Open("postgres", "...")
    defer db.Close()

    sf, err := stripeflow.New(stripeflow.Config{
        Dialect:         "postgres",  // "postgres", "mysql", "sqlite"
        DB:              db,
        StripeSecretKey: "sk_...",
        WebhookSecret:   "whsec_...",
        RedirectURL:     "https://example.com/account",
    }, &MyUserResolver{})
    if err != nil {
        log.Fatal(err)
    }

    // Sync Stripe prices to local DB
    sf.SyncPrices(context.Background())

    mux := http.NewServeMux()

    // Mount StripeFlow handlers:
    //   POST /stripe/checkout
    //   GET  /stripe/portal
    //   POST /stripe/webhook
    mux.Handle("/stripe/", http.StripPrefix("/stripe", sf.Handler()))

    // Protect routes with subscription middleware
    protected := sf.RequireSubscription(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "Payment Required", http.StatusPaymentRequired)
    })
    mux.Handle("/dashboard", protected(http.HandlerFunc(dashboardHandler)))

    log.Fatal(http.ListenAndServe(":8080", mux))
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("Welcome, subscriber!"))
}
```

### 4. Usage-Based Restrictions

If your Stripe products have usage limits in their metadata (e.g., `"max_api_calls": "1000"`), use `RequireUsage` or `CheckUsage` with a custom check function:

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "strconv"
    "net/http"

    "stripeflow"
)

// Your custom usage check — you control the logic entirely.
func checkAPICalls(ctx context.Context, sub *stripeflow.Subscription, plan *stripeflow.Plan) error {
    // Extract limit from plan metadata (synced from Stripe)
    limit := extractMetadataInt(plan.Metadata, "max_api_calls")
    if limit == 0 {
        return nil // no limit set, allow
    }

    // Count usage from YOUR data store
    used := countUserAPICalls(ctx, sub.UserID) // your logic
    if used >= limit {
        return fmt.Errorf("API call limit reached (%d/%d)", used, limit)
    }
    return nil
}

func extractMetadataInt(raw *json.RawMessage, key string) int64 {
    if raw == nil {
        return 0
    }
    var m map[string]string
    json.Unmarshal(*raw, &m)
    v, _ := strconv.ParseInt(m[key], 10, 64)
    return v
}

// As middleware:
mux.Handle("/api/", sf.RequireUsage(checkAPICalls, func(w http.ResponseWriter, r *http.Request) {
    http.Error(w, "Usage limit exceeded", http.StatusTooManyRequests)
})(http.HandlerFunc(apiHandler)))

// Or programmatically:
err := sf.CheckUsage(ctx, userID, checkAPICalls)
if err != nil {
    // usage limit exceeded
}
```

## API

### StripeFlow Methods

| Method | Description |
|--------|-------------|
| `Handler() http.Handler` | Returns an HTTP handler with checkout, portal, and webhook endpoints |
| `RequireSubscription(fallback) func(http.Handler) http.Handler` | Middleware that requires an active subscription |
| `RequireUsage(check, fallback) func(http.Handler) http.Handler` | Middleware that checks usage limits via a custom function |
| `CheckUsage(ctx, userID, check) error` | Programmatic usage check (non-middleware) |
| `SyncPrices(ctx) error` | Syncs all Stripe prices to the local database |
| `GetPlans(ctx) ([]Plan, error)` | Returns all active plans |
| `FindPlan(ctx, priceID) (*Plan, error)` | Finds a plan by Stripe price ID |
| `GetSubscription(ctx, userID) (*Subscription, error)` | Returns the user's latest subscription |
| `HasActiveSubscription(ctx, userID) (bool, error)` | Checks if a user has an active subscription |

### HTTP Endpoints

| Path | Method | Description |
|------|--------|-------------|
| `/checkout` | `POST` | Creates a Stripe Checkout session. Requires `plan_id` in form data. |
| `/portal` | `GET` | Creates a Stripe Billing Portal session for the current user. |
| `/webhook` | `POST` | Receives and processes Stripe webhook events. |

### Environment

Set these before initializing, or pass them directly via `Config`:

| Variable | Description |
|----------|-------------|
| `STRIPE_SECRET_KEY` | Your Stripe secret API key |
| `STRIPE_WEBHOOK_SECRET` | Webhook signing secret |

## Running Tests

```sh
# Unit tests (SQLite in-memory)
go test -v ./...

# Integration tests (requires Docker)
go test -v -run TestPostgresAndMySQLIntegration ./...
```
