# StripeFlow

StripeFlow is a plugable Golang library that helps developers synchronize products and handle payment flows with Stripe. It provides a structured architecture with isolated models, repositories for different SQL dialects (PostgreSQL, SQLite, MySQL), and service/handler layers.

## Architecture

StripeFlow is designed with flexibility and extensibility in mind:

- **Handler Layer (`handler/`)**: Contains HTTP handlers (using `chi`) for handling checkout, customer portal, and Stripe webhooks. It also provides middleware to protect endpoints by verifying active subscriptions and usages.
- **Service Layer (`service/`)**: Orchestrates business logic, synchronizes Stripe prices, and handles updates from Stripe webhook events.
- **Repository Layer (`repository/`)**: Defines a `Querier` interface. It leverages `github.com/stephenafamo/bob` to implement dialect-specific queries for Postgres, SQLite, and MySQL.
- **Migrations (`db/migrations/`)**: Contains SQL migration scripts and uses `github.com/pressly/goose` and `goose.fs` to embed them. Allows calling `MigrateUp` and `MigrateDown`.
- **Models (`db/models/`)**: Defines the data structures for `Plan` and `Subscription`.

## Installation

```sh
go get stripeflow
```

## Quick Start

### 1. Migrations

StripeFlow uses Goose to manage embedded schema migrations. You can use `MigrateUp` and `MigrateDown` to run the required table creation.
Example:

```go
package main

import (
	"database/sql"
	"log"
	"stripeflow/db/migrations"
	_ "github.com/lib/pq"
)

func main() {
	db, err := sql.Open("postgres", "postgres://user:pass@localhost:5432/dbname?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := migrations.MigrateUp(db, "postgres"); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
}
```

### 2. Initialization & Configuration

StripeFlow uses `github.com/josuebrunel/gopkg/xenv` for environment variables. Ensure the following are set:
- `STRIPEFLOW_STRIPE_SECRET_KEY`
- `STRIPEFLOW_STRIPE_WEBHOOK_SECRET`
- `STRIPEFLOW_CHECKOUT_REDIRECT`

You also need to provide a user resolver conforming to `handler.UserDetailsResolver` so StripeFlow can associate subscriptions to your local application users.

```go
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"stripeflow"
	"stripeflow/handler"
	_ "github.com/lib/pq" // or your choice of driver
)

// MyUserResolver implements handler.UserDetailsResolver
type MyUserResolver struct{}

func (r *MyUserResolver) GetUser(ctx context.Context) (string, error) {
    return "user-id-from-session", nil
}

func (r *MyUserResolver) GetUserDetails(ctx context.Context) (*handler.User, error) {
    return &handler.User{ID: "user-id", Email: "test@example.com"}, nil
}

func (r *MyUserResolver) GetUserByEmail(ctx context.Context, email string) (string, error) {
    return "user-id", nil
}

func main() {
	db, _ := sql.Open("postgres", "...")
	defer db.Close()

	cfg := stripeflow.Config{
		Dialect: "postgres", // 'postgres', 'sqlite', or 'mysql'
		DB:      db,
	}

	sf, err := stripeflow.New(cfg, &MyUserResolver{})
	if err != nil {
		log.Fatal(err)
	}

	// You can trigger a sync of Stripe prices manually or on a cron
	sf.Service.SyncPrices(context.Background())

	// Mount the HTTP handlers
	r := chi.NewRouter()

	// Handles:
	// POST /checkout (requires user session)
	// GET /portal (requires user session)
	// POST /webhook (receives Stripe events)
	sf.Handler.Mount(r)

	http.ListenAndServe(":8080", r)
}
```

### 3. Middleware

To protect routes in your application using StripeFlow:

```go
func ProtectedRoute() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("You have an active subscription!"))
    }
}

// Ensure the user has an active subscription
r.With(func(next http.Handler) http.Handler {
    return handler.SubscriptionActiveMiddleware(sf.Repo, &MyUserResolver{}, false, func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "Payment Required", http.StatusPaymentRequired)
    })(next)
}).Get("/protected", ProtectedRoute())
```

## Running Tests

StripeFlow includes unit tests and Docker-based integration tests. To run them:

```sh
go test -v ./...
```
