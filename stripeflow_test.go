package stripeflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v82"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"github.com/josuebrunel/stripeflow/migrations"
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

func newTestClient(t *testing.T, db *sql.DB, dialect Dialect) *Client {
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

	// Insert an initial subscription.
	now := time.Now().UTC()
	if err := sf.repo.upsertSubscription(ctx, upsertSubParams{
		UserID: "user-123",
		Status: StatusNone,
	}); err != nil {
		t.Fatalf("upsert initial subscription: %v", err)
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

	m := json.RawMessage(`{"tier":"pro"}`)
	f := json.RawMessage(`[{"name":"Cool feature"}]`)

	if err := sf.repo.upsertProduct(ctx, Product{
		ID:              "prod_test",
		Name:            "Pro Plan",
		Description:     "The pro plan",
		Active:          true,
		Metadata:        &m,
		Features:        &f,
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
	if string(*products[0].Metadata) != string(m) {
		t.Fatalf("expected metadata %s, got %s", m, *products[0].Metadata)
	}
	if string(*products[0].Features) != string(f) {
		t.Fatalf("expected features %s, got %s", f, *products[0].Features)
	}

	// Upsert a price.
	ua := int64(1999)
	count := 1
	pm := json.RawMessage(`{"type":"recurring"}`)
	if err := sf.repo.upsertPrice(ctx, Price{
		ID:                "price_test",
		ProductID:         "prod_test",
		Currency:          "usd",
		UnitAmount:        &ua,
		RecurringInterval: "month",
		RecurringCount:    &count,
		UsageType:         "licensed",
		Type:              "recurring",
		Nickname:          "Pro Plan — monthly",
		Active:            true,
		Metadata:          &pm,
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
	if string(*prices[0].Metadata) != string(pm) {
		t.Fatalf("expected price metadata %s, got %s", pm, *prices[0].Metadata)
	}
}

// --------------------------------------------------------------------------
// Middleware tests
// --------------------------------------------------------------------------

func testMiddleware(t *testing.T, sf *Client) {
	t.Helper()
	ctx := context.Background()

	// Seed a user with an active subscription.
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
// Checkout Handler
// --------------------------------------------------------------------------

func testCheckoutHandler(t *testing.T, sf *Client) {
	t.Helper()

	req := httptest.NewRequest("POST", "/checkout", nil)
	rr := httptest.NewRecorder()

	// Should fail because GetUserID is not in context/header
	sf.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	// Set required params but mock get user ID
	req = httptest.NewRequest("POST", "/checkout?plan_id=price_test&success_url=http://example.com/success&cancel_url=http://example.com/cancel", nil)
	req.Header.Set("X-User-ID", "mw-user")
	rr = httptest.NewRecorder()

	// Should fail trying to talk to stripe but it passes the handler validation
	// It should return 500 because it attempts to talk to Stripe with fake keys
	sf.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
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

	// Redelivery before the first attempt finished → must be retryable, not
	// silently swallowed (e.g. the process crashed mid-handling).
	already, err = sf.repo.markEventProcessing(ctx, "evt_test_001", "invoice.payment_succeeded")
	if err != nil {
		t.Fatalf("markEventProcessing (before done): %v", err)
	}
	if already {
		t.Fatal("expected alreadyProcessed=false while the first attempt is still unfinished")
	}

	// Mark done (success).
	if err := sf.repo.markEventDone(ctx, "evt_test_001", nil); err != nil {
		t.Fatalf("markEventDone: %v", err)
	}

	// Redelivery after a successful attempt → skipped.
	already, err = sf.repo.markEventProcessing(ctx, "evt_test_001", "invoice.payment_succeeded")
	if err != nil {
		t.Fatalf("markEventProcessing (after done): %v", err)
	}
	if !already {
		t.Fatal("expected alreadyProcessed=true after a successful attempt")
	}
}

// --------------------------------------------------------------------------
// SQLite (fast, in-memory)
// --------------------------------------------------------------------------

func TestSQLite(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()

	sf := newTestClient(t, db, SQLite)

	t.Run("CoreOperations", func(t *testing.T) { testCoreOperations(t, sf) })
	t.Run("ProductOperations", func(t *testing.T) { testProductOperations(t, sf) })
	t.Run("Middleware", func(t *testing.T) { testMiddleware(t, sf) })
	t.Run("CheckoutHandler", func(t *testing.T) { testCheckoutHandler(t, sf) })
	t.Run("WebhookIdempotency", func(t *testing.T) { testWebhookIdempotency(t, sf) })
	t.Run("HelperMethods", testHelperMethods(sf))
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
		sf := newTestClient(t, db, Postgres)
		t.Run("CoreOperations", func(t *testing.T) { testCoreOperations(t, sf) })
		t.Run("ProductOperations", func(t *testing.T) { testProductOperations(t, sf) })
		t.Run("Middleware", func(t *testing.T) { testMiddleware(t, sf) })
		t.Run("CheckoutHandler", func(t *testing.T) { testCheckoutHandler(t, sf) })
		t.Run("WebhookIdempotency", func(t *testing.T) { testWebhookIdempotency(t, sf) })
		t.Run("HelperMethods", testHelperMethods(sf))
	})

	t.Run("MySQL", func(t *testing.T) {
		db := setupTestDB(t, "mysql", "testuser:testpassword@tcp(127.0.0.1:3306)/testdb?parseTime=true")
		defer db.Close()
		sf := newTestClient(t, db, MySQL)
		t.Run("CoreOperations", func(t *testing.T) { testCoreOperations(t, sf) })
		t.Run("ProductOperations", func(t *testing.T) { testProductOperations(t, sf) })
		t.Run("Middleware", func(t *testing.T) { testMiddleware(t, sf) })
		t.Run("CheckoutHandler", func(t *testing.T) { testCheckoutHandler(t, sf) })
		t.Run("WebhookIdempotency", func(t *testing.T) { testWebhookIdempotency(t, sf) })
		t.Run("HelperMethods", testHelperMethods(sf))
	})
}

func testHelperMethods(sf *Client) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		// 1. Setup subscription
		params := upsertSubParams{
			UserID:               "helper-user",
			StripeCustomerID:     "cus_helper123",
			StripeSubscriptionID: "sub_helper456",
			Status:               StatusActive,
		}
		err := sf.repo.upsertSubscription(ctx, params)
		if err != nil {
			t.Fatalf("upsert sub: %v", err)
		}

		// Retrieve by UserID
		sub1, err := sf.GetSubscription(ctx, "helper-user")
		if err != nil {
			t.Fatalf("GetSubscription: %v", err)
		}

		// Retrieve by ID
		sub2, err := sf.GetSubscriptionByID(ctx, sub1.ID)
		if err != nil {
			t.Fatalf("GetSubscriptionByID: %v", err)
		}
		if sub1.ID != sub2.ID {
			t.Errorf("expected id %v, got %v", sub1.ID, sub2.ID)
		}

		// Retrieve by Customer ID
		sub3, err := sf.GetSubscriptionByCustomerID(ctx, "cus_helper123")
		if err != nil {
			t.Fatalf("GetSubscriptionByCustomerID: %v", err)
		}
		if sub1.ID != sub3.ID {
			t.Errorf("expected id %v, got %v", sub1.ID, sub3.ID)
		}

		// Retrieve by Stripe Sub ID
		sub4, err := sf.GetSubscriptionByStripeSubID(ctx, "sub_helper456")
		if err != nil {
			t.Fatalf("GetSubscriptionByStripeSubID: %v", err)
		}
		if sub1.ID != sub4.ID {
			t.Errorf("expected id %v, got %v", sub1.ID, sub4.ID)
		}

		// 2. Setup product
		p := Product{
			ID:     "prod_helper_xyz",
			Name:   "Helper Product",
			Active: true,
		}
		if err := sf.repo.upsertProduct(ctx, p); err != nil {
			t.Fatalf("upsert product: %v", err)
		}

		// Retrieve by ID
		prod1, err := sf.GetProductByID(ctx, "prod_helper_xyz")
		if err != nil {
			t.Fatalf("GetProductByID: %v", err)
		}
		if prod1.Name != p.Name {
			t.Errorf("expected name %s, got %s", p.Name, prod1.Name)
		}
	}
}

func TestDeleteOperations(t *testing.T) {
	t.Run("DeleteProducts", func(t *testing.T) {
		ctx := context.Background()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		repo, err := newRepository(db, string(SQLite))
		if err != nil {
			t.Fatal(err)
		}
		client := &Client{repo: repo}
		// Create the schema manually for tests
		_, err = db.Exec(`CREATE TABLE stripeflow_products (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT,
			active INTEGER NOT NULL DEFAULT 1,
			metadata TEXT NOT NULL DEFAULT '{}',
			features TEXT NOT NULL DEFAULT '[]',
			stripe_created_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`CREATE TABLE stripeflow_prices (
			id TEXT PRIMARY KEY, product_id TEXT NOT NULL, currency TEXT NOT NULL,
			unit_amount INTEGER, recurring_interval TEXT, recurring_count INTEGER,
			usage_type TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL DEFAULT '',
			lookup_key TEXT NOT NULL DEFAULT '',
			active INTEGER NOT NULL DEFAULT 1,
			metadata TEXT NOT NULL DEFAULT '{}',
			stripe_created_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`)
		if err != nil {
			t.Fatal(err)
		}

		// Insert dummy data
		_, err = db.Exec("INSERT INTO stripeflow_products (id, name, active) VALUES (?, ?, ?)", "prod_1", "Product 1", 1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec("INSERT INTO stripeflow_prices (id, product_id, currency, active) VALUES (?, ?, ?, ?)", "price_1", "prod_1", "usd", 1)
		if err != nil {
			t.Fatal(err)
		}

		err = client.repo.deleteProduct(ctx, "prod_1")
		if err != nil {
			t.Fatal(err)
		}

		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM stripeflow_products").Scan(&count)
		err = db.QueryRow("SELECT COUNT(*) FROM stripeflow_prices").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected 0 prices, got %d", count)
		}
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected 0 products, got %d", count)
		}

		// Now Test DeleteAllProducts
		_, err = db.Exec("INSERT INTO stripeflow_products (id, name, active) VALUES (?, ?, ?)", "prod_2", "Product 2", 1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec("INSERT INTO stripeflow_prices (id, product_id, currency, active) VALUES (?, ?, ?, ?)", "price_2", "prod_2", "usd", 1)
		if err != nil {
			t.Fatal(err)
		}

		err = client.repo.deleteAllProducts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = db.QueryRow("SELECT COUNT(*) FROM stripeflow_products").Scan(&count)
		err = db.QueryRow("SELECT COUNT(*) FROM stripeflow_prices").Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected 0 prices, got %d", count)
		}
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected 0 products, got %d", count)
		}
	})
}

func TestUpsertIdempotency(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)

	userID := "test-user-idempotency"
	custID := "cus_123"
	subID := "sub_456"

	// 1. Initial upsert with customer ID
	err := sf.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:           userID,
		StripeCustomerID: custID,
		Status:           StatusNone,
	})
	if err != nil {
		t.Fatalf("Initial upsert failed: %v", err)
	}

	sub, err := sf.GetSubscription(ctx, userID)
	if err != nil {
		t.Fatalf("GetSubscription failed: %v", err)
	}
	if sub.StripeCustomerID != custID {
		t.Fatalf("Expected customer ID %s, got %s", custID, sub.StripeCustomerID)
	}

	// 2. Upsert with subscription ID but empty customer ID
	err = sf.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               userID,
		StripeSubscriptionID: subID,
		Status:               StatusActive,
	})
	if err != nil {
		t.Fatalf("Second upsert failed: %v", err)
	}

	sub, err = sf.GetSubscription(ctx, userID)
	if err != nil {
		t.Fatalf("GetSubscription failed: %v", err)
	}
	if sub.StripeCustomerID != custID {
		t.Fatalf("Customer ID was cleared! Expected %s, got %s", custID, sub.StripeCustomerID)
	}
	if sub.StripeSubscriptionID != subID {
		t.Fatalf("Expected subscription ID %s, got %s", subID, sub.StripeSubscriptionID)
	}
	if sub.Status != StatusActive {
		t.Fatalf("Expected status active, got %s", sub.Status)
	}
}

// TestWebhookEventRetry verifies that a failed or never-finished processing
// attempt is retryable on redelivery, while a successfully processed event
// is correctly skipped. Previously markEventProcessing only checked whether
// the event row existed, so ANY prior attempt (success or failure, finished
// or crashed mid-handling) permanently blocked reprocessing on retry.
func TestWebhookEventRetry(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)

	// A never-finished attempt (e.g. the process crashed mid-handling, or a
	// panic occurred before recovery was added) must be retryable.
	already, err := sf.repo.markEventProcessing(ctx, "evt_stuck", "customer.subscription.updated")
	if err != nil {
		t.Fatalf("markEventProcessing (first attempt): %v", err)
	}
	if already {
		t.Fatalf("expected a fresh event to not be marked already-processed")
	}
	// Simulate a crash: markEventDone is never called for this attempt.
	already, err = sf.repo.markEventProcessing(ctx, "evt_stuck", "customer.subscription.updated")
	if err != nil {
		t.Fatalf("markEventProcessing (retry after crash): %v", err)
	}
	if already {
		t.Fatalf("a never-finished attempt must be retryable, but was reported as already processed")
	}

	// A finished attempt that failed must also be retryable.
	if err := sf.repo.markEventDone(ctx, "evt_stuck", errors.New("transient db error")); err != nil {
		t.Fatalf("markEventDone: %v", err)
	}
	already, err = sf.repo.markEventProcessing(ctx, "evt_stuck", "customer.subscription.updated")
	if err != nil {
		t.Fatalf("markEventProcessing (retry after failure): %v", err)
	}
	if already {
		t.Fatalf("a failed attempt must be retryable, but was reported as already processed")
	}

	// A finished, successful attempt must be skipped on redelivery.
	if err := sf.repo.markEventDone(ctx, "evt_stuck", nil); err != nil {
		t.Fatalf("markEventDone (success): %v", err)
	}
	already, err = sf.repo.markEventProcessing(ctx, "evt_stuck", "customer.subscription.updated")
	if err != nil {
		t.Fatalf("markEventProcessing (redelivery after success): %v", err)
	}
	if !already {
		t.Fatalf("a successfully processed event should be skipped on redelivery")
	}
}

// TestWebhookHandlerErrorSemantics verifies that event handlers distinguish
// permanent conditions (unknown/missing customer — retrying can't help, so
// the event should be dropped with a nil error) from transient failures
// (e.g. a DB error — the event should propagate an error so the caller
// returns 500 and Stripe retries).
func TestWebhookHandlerErrorSemantics(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t, "sqlite", ":memory:")
	sf := newTestClient(t, db, SQLite)

	unknownSub := &stripe.Subscription{
		ID:       "sub_unknown",
		Customer: &stripe.Customer{ID: "cus_unknown"},
		Status:   stripe.SubscriptionStatusActive,
	}
	if err := sf.onSubscriptionUpdated(ctx, unknownSub); err != nil {
		t.Errorf("onSubscriptionUpdated: expected nil for unknown customer (permanent), got %v", err)
	}
	if err := sf.onSubscriptionDeleted(ctx, unknownSub); err != nil {
		t.Errorf("onSubscriptionDeleted: expected nil for unknown customer (permanent), got %v", err)
	}

	unknownInv := &stripe.Invoice{Customer: &stripe.Customer{ID: "cus_unknown"}}
	if err := sf.onInvoicePaymentSucceeded(ctx, unknownInv); err != nil {
		t.Errorf("onInvoicePaymentSucceeded: expected nil for unknown customer (permanent), got %v", err)
	}
	if err := sf.onInvoicePaymentFailed(ctx, unknownInv); err != nil {
		t.Errorf("onInvoicePaymentFailed: expected nil for unknown customer (permanent), got %v", err)
	}

	// A subscription event with no Customer at all must not panic and must
	// be treated as a permanent no-op.
	noCustomerSub := &stripe.Subscription{ID: "sub_no_customer"}
	if err := sf.onSubscriptionUpdated(ctx, noCustomerSub); err != nil {
		t.Errorf("onSubscriptionUpdated: expected nil for missing customer, got %v", err)
	}
	if err := sf.onSubscriptionDeleted(ctx, noCustomerSub); err != nil {
		t.Errorf("onSubscriptionDeleted: expected nil for missing customer, got %v", err)
	}

	// A transient failure (DB unavailable) must propagate as a real error,
	// not be swallowed like the permanent case above.
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if err := sf.onSubscriptionUpdated(ctx, unknownSub); err == nil {
		t.Error("onSubscriptionUpdated: expected transient DB error to propagate, got nil")
	}
	if err := sf.onSubscriptionDeleted(ctx, unknownSub); err == nil {
		t.Error("onSubscriptionDeleted: expected transient DB error to propagate, got nil")
	}
	if err := sf.onInvoicePaymentSucceeded(ctx, unknownInv); err == nil {
		t.Error("onInvoicePaymentSucceeded: expected transient DB error to propagate, got nil")
	}
	if err := sf.onInvoicePaymentFailed(ctx, unknownInv); err == nil {
		t.Error("onInvoicePaymentFailed: expected transient DB error to propagate, got nil")
	}
}

// TestClearStripeLinkage verifies that clearing a stale Stripe linkage (used
// when Stripe reports the customer as deleted) resets only the Stripe-side
// fields and status, preserving usage history rather than destroying the row
// the way deleteSubscription does.
func TestClearStripeLinkage(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)

	userID := "test-user-clear-linkage"
	if err := sf.repo.upsertSubscription(ctx, upsertSubParams{
		UserID:               userID,
		StripeCustomerID:     "cus_gone",
		StripeSubscriptionID: "sub_gone",
		StripePriceID:        "price_gone",
		StripeProductID:      "prod_gone",
		Status:               StatusActive,
	}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	if _, err := sf.repo.incrementUsage(ctx, userID, 42); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	if err := sf.repo.clearStripeLinkage(ctx, userID); err != nil {
		t.Fatalf("clearStripeLinkage: %v", err)
	}

	sub, err := sf.repo.getSubscriptionByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("getSubscriptionByUserID: %v", err)
	}
	if sub.StripeCustomerID != "" || sub.StripeSubscriptionID != "" || sub.StripePriceID != "" || sub.StripeProductID != "" {
		t.Fatalf("expected all Stripe linkage fields cleared, got %+v", sub)
	}
	if sub.Status != StatusNone {
		t.Fatalf("expected status reset to none, got %s", sub.Status)
	}
	if sub.UsageCount != 42 {
		t.Fatalf("expected usage history preserved (42), got %d", sub.UsageCount)
	}
}
