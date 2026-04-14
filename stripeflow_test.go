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
	"github.com/josuebrunel/gopkg/assert"
	"stripeflow/db/migrations"
	"stripeflow/db/models"
	"stripeflow/handler"
)

type MockUserResolver struct{}

func (m *MockUserResolver) GetUser(ctx context.Context) (string, error) {
	return "user-123", nil
}

func (m *MockUserResolver) GetUserDetails(ctx context.Context) (*handler.User, error) {
	return &handler.User{ID: "user-123", Email: "test@example.com"}, nil
}

func (m *MockUserResolver) GetUserByEmail(ctx context.Context, email string) (string, error) {
	return "user-123", nil
}

func setupTestDB(t *testing.T, dialect, dsn string) *sql.DB {
	db, err := sql.Open(dialect, dsn)
	assert.Eq(t, err, nil)

	err = db.Ping()
	assert.Eq(t, err, nil)

	dialectName := dialect
	if dialect == "sqlite" {
		dialectName = "sqlite3"
	}

	err = migrations.MigrateDown(db, dialectName)
	if err != nil {
		t.Logf("Failed to migrate down, continuing: %v", err)
	}

	err = migrations.MigrateUp(db, dialectName)
	assert.Eq(t, err, nil)

	return db
}

func testLibraryOperations(t *testing.T, sf *StripeFlow) {
	ctx := context.Background()

	// Test Upsert Plan
	plan := &models.Plan{
		Name:            "Test Plan",
		Slug:            "test-plan",
		StripeProductID: "prod_123",
		StripePriceID:   "price_123",
		PriceUsd:        1000,
		IsActive:        true,
		BillingCycle:    "month",
		SortOrder:       1,
	}

	insertedPlan, err := sf.Repo.UpsertPlan(ctx, plan)
	assert.Eq(t, err, nil)
	if insertedPlan == nil {
		t.Fatalf("expected not nil but got nil")
	}
	assert.Eq(t, "test-plan", insertedPlan.Slug)

	// Test Find Plan
	foundPlan, err := sf.Repo.FindPlan(ctx, "price_123")
	assert.Eq(t, err, nil)
	if foundPlan == nil {
		t.Fatalf("expected not nil but got nil")
	}
	assert.Eq(t, "Test Plan", foundPlan.Name)

	// Test Upsert Subscription
	sub := &models.Subscription{
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

	insertedSub, err := sf.Repo.UpsertSubscription(ctx, sub)
	assert.Eq(t, err, nil)
	if insertedSub == nil {
		t.Fatalf("expected not nil but got nil")
	}

	// Test Delete Subscription
	err = sf.Repo.DeleteSubscription(ctx, insertedSub.ID)
	assert.Eq(t, err, nil)

	deletedSub, err := sf.Repo.FindSubscriptionByUserID(ctx, "user-123")
	if err == nil {
		t.Fatalf("expected error but got nil")
	}
	if deletedSub != nil {
		t.Fatalf("expected nil but got %v", deletedSub)
	}
}

func TestSQLite(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()

	cfg := Config{
		Dialect: "sqlite",
		DB:      db,
	}

	resolver := &MockUserResolver{}
	sf, err := New(cfg, resolver)
	assert.Eq(t, err, nil)

	testLibraryOperations(t, sf)
}

func TestPostgresAndMySQLIntegration(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI")
	}

	cmd := exec.Command("docker", "compose", "up", "-d")
	err := cmd.Run()
	if err != nil {
		t.Skipf("docker compose up failed, skipping integration test: %v", err)
	}

	defer func() {
		exec.Command("docker", "compose", "down", "-v").Run()
	}()

	time.Sleep(10 * time.Second) // wait for db to start

	// Test Postgres
	pgDSN := "postgres://testuser:testpassword@localhost:5432/testdb?sslmode=disable"
	pgDB := setupTestDB(t, "postgres", pgDSN)
	defer pgDB.Close()

	pgCfg := Config{
		Dialect: "postgres",
		DB:      pgDB,
	}

	sfPg, err := New(pgCfg, &MockUserResolver{})
	assert.Eq(t, err, nil)

	t.Run("Postgres", func(t *testing.T) {
		testLibraryOperations(t, sfPg)
	})

	// Test MySQL
	mysqlDSN := "testuser:testpassword@tcp(127.0.0.1:3306)/testdb?parseTime=true"
	mysqlDB := setupTestDB(t, "mysql", mysqlDSN)
	defer mysqlDB.Close()

	mysqlCfg := Config{
		Dialect: "mysql",
		DB:      mysqlDB,
	}

	sfMysql, err := New(mysqlCfg, &MockUserResolver{})
	assert.Eq(t, err, nil)

	t.Run("MySQL", func(t *testing.T) {
		testLibraryOperations(t, sfMysql)
	})
}
