package stripeflow

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"stripeflow/migrations"
)

type mockResolver struct{}

func (m *mockResolver) GetUserID(ctx context.Context) (string, error) {
	return "user-123", nil
}

func (m *mockResolver) GetUserEmail(ctx context.Context) (string, error) {
	return "test@example.com", nil
}

func (m *mockResolver) FindUserIDByEmail(ctx context.Context, email string) (string, error) {
	return "user-123", nil
}

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

func testOperations(t *testing.T, sf *StripeFlow) {
	ctx := context.Background()

	// Test Upsert Plan
	plan := &Plan{
		Name:            "Test Plan",
		Slug:            "test-plan",
		StripeProductID: "prod_123",
		StripePriceID:   "price_123",
		PriceUsd:        1000,
		IsActive:        true,
		BillingCycle:    "month",
		SortOrder:       1,
	}

	inserted, err := sf.repo.upsertPlan(ctx, plan)
	if err != nil {
		t.Fatalf("upsert plan: %v", err)
	}
	if inserted == nil {
		t.Fatal("expected plan, got nil")
	}
	if inserted.Slug != "test-plan" {
		t.Fatalf("expected slug test-plan, got %s", inserted.Slug)
	}

	// Test Find Plan
	found, err := sf.FindPlan(ctx, "price_123")
	if err != nil {
		t.Fatalf("find plan: %v", err)
	}
	if found.Name != "Test Plan" {
		t.Fatalf("expected name Test Plan, got %s", found.Name)
	}

	// Test Upsert Subscription
	sub := &Subscription{
		StripeCustomerID:     "cus_123",
		StripeSubscriptionID: "sub_123",
		StripePriceID:        "price_123",
		UserID:               "user-123",
		PlanName:             "Test Plan",
		Status:               "active",
		DateStart:            time.Now().UTC(),
		DateEnd:              time.Now().AddDate(0, 1, 0).UTC(),
		DateRenewal:          time.Now().AddDate(0, 1, 0).UTC(),
	}

	insertedSub, err := sf.repo.upsertSubscription(ctx, sub)
	if err != nil {
		t.Fatalf("upsert subscription: %v", err)
	}
	if insertedSub == nil {
		t.Fatal("expected subscription, got nil")
	}

	// Test HasActiveSubscription
	active, err := sf.HasActiveSubscription(ctx, "user-123")
	if err != nil {
		t.Fatalf("check active: %v", err)
	}
	if !active {
		t.Fatal("expected active subscription")
	}

	// Test Delete Subscription
	if err := sf.repo.deleteSubscription(ctx, insertedSub.ID); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}

	_, err = sf.GetSubscription(ctx, "user-123")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestSQLite(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()

	sf, err := New(Config{
		Dialect: "sqlite",
		DB:      db,
	}, &mockResolver{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	testOperations(t, sf)
}

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

		sf, err := New(Config{Dialect: "postgres", DB: db}, &mockResolver{})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		testOperations(t, sf)
	})

	t.Run("MySQL", func(t *testing.T) {
		db := setupTestDB(t, "mysql", "testuser:testpassword@tcp(127.0.0.1:3306)/testdb?parseTime=true")
		defer db.Close()

		sf, err := New(Config{Dialect: "mysql", DB: db}, &mockResolver{})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		testOperations(t, sf)
	})
}
