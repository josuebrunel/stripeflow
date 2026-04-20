package stripeflow

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"stripeflow/migrations"
)

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

func setupTestDB(t *testing.T, dialect, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(dialect, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	gooseDialect := dialect
	if dialect == "sqlite" {
		gooseDialect = "sqlite3"
	}

	_ = migrations.MigrateDown(db, gooseDialect)

	if err := migrations.MigrateUp(db, gooseDialect); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return db
}

func newTestClient(t *testing.T, db *sql.DB, dialect string) *Client {
	t.Helper()
	sf, err := New(Config{
		Dialect:         dialect,
		DB:              db,
		StripeSecretKey: "sk_test_placeholder",
		WebhookSecret:   "whsec_placeholder",
		GetUserID: func(r *http.Request) (string, error) {
			uid := r.Header.Get("X-User-ID")
			if uid == "" {
				return "", errors.New("missing X-User-ID")
			}
			return uid, nil
		},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return sf
}

// --------------------------------------------------------------------------
// Core repository operations
// --------------------------------------------------------------------------

func testCoreOperations(t *testing.T, sf *Client) {
	t.Helper()
	ctx := context.Background()

	// Create empty subscription row for user.
	if err := sf.repo.createEmptySubscription(ctx, "user-123"); err != nil {
		t.Fatalf("createEmptySubscription: %v", err)
	}

	// Subscription should be in 'none' state.
	sub, err := sf.GetSubscription(ctx, "user-123")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if sub.Status != StatusNone {
		t.Fatalf("expected status 'none', got %s", sub.Status)
	}

	// Upsert an active subscription.
	now := time.Now().UTC()
	end := now.AddDate(0, 1, 0)
	if err := sf.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               "user-123",
		StripeCustomerID:     "cus_test",
		StripeSubscriptionID: "sub_test",
		StripePriceID:        "price_test",
		Status:               StatusActive,
		CurrentPeriodStart:   &now,
		CurrentPeriodEnd:     &end,
	}); err != nil {
		t.Fatalf("upsertSubscription: %v", err)
	}

	// Verify active status.
	sub, err = sf.GetSubscription(ctx, "user-123")
	if err != nil {
		t.Fatalf("GetSubscription after upsert: %v", err)
	}
	if sub.Status != StatusActive {
		t.Fatalf("expected status 'active', got %s", sub.Status)
	}
	if sub.StripeCustomerID != "cus_test" {
		t.Fatalf("expected customer cus_test, got %s", sub.StripeCustomerID)
	}

	// Lookup by customer ID.
	byCustomer, err := sf.repo.getSubscriptionByCustomerID(ctx, "cus_test")
	if err != nil {
		t.Fatalf("getSubscriptionByCustomerID: %v", err)
	}
	if byCustomer.UserID != "user-123" {
		t.Fatalf("expected user-123, got %s", byCustomer.UserID)
	}

	// Usage tracking.
	count, err := sf.IncrementUsage(ctx, "user-123", 5)
	if err != nil {
		t.Fatalf("IncrementUsage: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected usage_count=5, got %d", count)
	}

	limit := int64(10)
	if err := sf.SetUsageLimit(ctx, "user-123", &limit); err != nil {
		t.Fatalf("SetUsageLimit: %v", err)
	}

	sub, _ = sf.GetSubscription(ctx, "user-123")
	if sub.UsageLimitReached() {
		t.Fatal("usage limit should not be reached yet (5/10)")
	}

	if err := sf.ResetUsage(ctx, "user-123"); err != nil {
		t.Fatalf("ResetUsage: %v", err)
	}
	sub, _ = sf.GetSubscription(ctx, "user-123")
	if sub.UsageCount != 0 {
		t.Fatalf("expected usage_count=0 after reset, got %d", sub.UsageCount)
	}
}

// --------------------------------------------------------------------------
// Product & Price operations
// --------------------------------------------------------------------------

func testProductOperations(t *testing.T, sf *Client) {
	t.Helper()
	ctx := context.Background()

	// Upsert a product.
	now := time.Now().UTC()
	if err := sf.repo.upsertProduct(ctx, Product{
		ID:              "prod_test",
		Name:            "Pro Plan",
		Description:     "The pro plan",
		Active:          true,
		StripeCreatedAt: &now,
	}); err != nil {
		t.Fatalf("upsertProduct: %v", err)
	}

	products, err := sf.ListProducts(ctx, true)
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(products) != 1 || products[0].ID != "prod_test" {
		t.Fatalf("expected 1 product 'prod_test', got %+v", products)
	}

	// Upsert a price.
	ua := int64(1999)
	count := 1
	if err := sf.repo.upsertPrice(ctx, Price{
		ID:                "price_test",
		ProductID:         "prod_test",
		Currency:          "usd",
		UnitAmount:        &ua,
		RecurringInterval: "month",
		RecurringCount:    &count,
		Active:            true,
		StripeCreatedAt:   &now,
	}); err != nil {
		t.Fatalf("upsertPrice: %v", err)
	}

	prices, err := sf.ListPrices(ctx, "prod_test")
	if err != nil {
		t.Fatalf("ListPrices: %v", err)
	}
	if len(prices) != 1 || prices[0].ID != "price_test" {
		t.Fatalf("expected 1 price 'price_test', got %+v", prices)
	}
}

// --------------------------------------------------------------------------
// Middleware tests
// --------------------------------------------------------------------------

func testMiddleware(t *testing.T, sf *Client) {
	t.Helper()
	ctx := context.Background()

	// Seed a user with an active subscription.
	if err := sf.repo.createEmptySubscription(ctx, "mw-user"); err != nil {
		t.Fatalf("createEmptySubscription: %v", err)
	}
	now := time.Now().UTC()
	end := now.AddDate(0, 1, 0)
	if err := sf.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               "mw-user",
		StripeCustomerID:     "cus_mw",
		StripeSubscriptionID: "sub_mw",
		Status:               StatusActive,
		CurrentPeriodStart:   &now,
		CurrentPeriodEnd:     &end,
	}); err != nil {
		t.Fatalf("upsertSubscription: %v", err)
	}

	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		sub, ok := SubscriptionFromContext(r.Context())
		if !ok || sub == nil {
			t.Error("SubscriptionFromContext: expected subscription in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	// Request with valid user → inner handler called.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "mw-user")
	rr := httptest.NewRecorder()
	sf.RequireActiveOrTrial(inner).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !innerCalled {
		t.Fatal("inner handler was not called")
	}

	// Request with no user → 402.
	req2 := httptest.NewRequest("GET", "/", nil)
	rr2 := httptest.NewRecorder()
	sf.RequireActiveOrTrial(inner).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d", rr2.Code)
	}

	// Request with unknown user → 402.
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("X-User-ID", "nobody")
	rr3 := httptest.NewRecorder()
	sf.RequireActiveOrTrial(inner).ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402 for unknown user, got %d", rr3.Code)
	}
}

// --------------------------------------------------------------------------
// Webhook idempotency
// --------------------------------------------------------------------------

func testWebhookIdempotency(t *testing.T, sf *Client) {
	t.Helper()
	ctx := context.Background()

	// First insert → not already processed.
	already, err := sf.repo.markEventProcessing(ctx, "evt_test_001", "invoice.payment_succeeded")
	if err != nil {
		t.Fatalf("markEventProcessing: %v", err)
	}
	if already {
		t.Fatal("expected alreadyProcessed=false on first insert")
	}

	// Second insert → already processed.
	already, err = sf.repo.markEventProcessing(ctx, "evt_test_001", "invoice.payment_succeeded")
	if err != nil {
		t.Fatalf("markEventProcessing (second): %v", err)
	}
	if !already {
		t.Fatal("expected alreadyProcessed=true on duplicate")
	}

	// Mark done.
	if err := sf.repo.markEventDone(ctx, "evt_test_001", nil); err != nil {
		t.Fatalf("markEventDone: %v", err)
	}
}

// --------------------------------------------------------------------------
// SQLite (fast, in-memory)
// --------------------------------------------------------------------------

func TestSQLite(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()

	sf := newTestClient(t, db, "sqlite")

	t.Run("CoreOperations", func(t *testing.T) { testCoreOperations(t, sf) })
	t.Run("ProductOperations", func(t *testing.T) { testProductOperations(t, sf) })
	t.Run("Middleware", func(t *testing.T) { testMiddleware(t, sf) })
	t.Run("WebhookIdempotency", func(t *testing.T) { testWebhookIdempotency(t, sf) })
}

// --------------------------------------------------------------------------
// Postgres + MySQL integration (requires Docker)
// --------------------------------------------------------------------------

func TestPostgresAndMySQLIntegration(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI")
	}

	cmd := exec.Command("docker", "compose", "up", "-d")
	if err := cmd.Run(); err != nil {
		t.Skipf("docker compose up failed, skipping: %v", err)
	}
	defer exec.Command("docker", "compose", "down", "-v").Run()

	time.Sleep(10 * time.Second)

	t.Run("Postgres", func(t *testing.T) {
		db := setupTestDB(t, "postgres", "postgres://testuser:testpassword@localhost:5432/testdb?sslmode=disable")
		defer db.Close()
		sf := newTestClient(t, db, "postgres")
		t.Run("CoreOperations", func(t *testing.T) { testCoreOperations(t, sf) })
		t.Run("ProductOperations", func(t *testing.T) { testProductOperations(t, sf) })
		t.Run("Middleware", func(t *testing.T) { testMiddleware(t, sf) })
		t.Run("WebhookIdempotency", func(t *testing.T) { testWebhookIdempotency(t, sf) })
	})

	t.Run("MySQL", func(t *testing.T) {
		db := setupTestDB(t, "mysql", "testuser:testpassword@tcp(127.0.0.1:3306)/testdb?parseTime=true")
		defer db.Close()
		sf := newTestClient(t, db, "mysql")
		t.Run("CoreOperations", func(t *testing.T) { testCoreOperations(t, sf) })
		t.Run("ProductOperations", func(t *testing.T) { testProductOperations(t, sf) })
		t.Run("Middleware", func(t *testing.T) { testMiddleware(t, sf) })
		t.Run("WebhookIdempotency", func(t *testing.T) { testWebhookIdempotency(t, sf) })
	})
}
