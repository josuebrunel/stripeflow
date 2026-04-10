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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)

	err = db.Ping()
	require.NoError(t, err)

	dialectName := dialect
	if dialect == "sqlite" {
		dialectName = "sqlite3"
	}

	err = migrations.MigrateDown(db, dialectName)
	if err != nil {
		t.Logf("Failed to migrate down, continuing: %v", err)
	}

	err = migrations.MigrateUp(db, dialectName)
	require.NoError(t, err)

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
		MaxDescriptions: 10,
		MaxPhotos:       50,
	}

	insertedPlan, err := sf.Repo.UpsertPlan(ctx, plan)
	assert.NoError(t, err)
	assert.NotNil(t, insertedPlan)
	assert.Equal(t, "test-plan", insertedPlan.Slug)

	// Test Find Plan
	foundPlan, err := sf.Repo.FindPlan(ctx, "price_123")
	assert.NoError(t, err)
	assert.NotNil(t, foundPlan)
	assert.Equal(t, "Test Plan", foundPlan.Name)

	// Test Upsert Subscription
	sub := &models.Subscription{
		StripeCustomerID:     "cus_123",
		StripeSubscriptionID: "sub_123",
		StripePriceID:        "price_123",
		UserID:               "user-123",
		PlanName:             "Test Plan",
		Status:               "active",
		UsageDesc:            10,
		UsagePhotos:          50,
		DateStart:            time.Now().UTC(),
		DateEnd:              time.Now().AddDate(0, 1, 0).UTC(),
		DateRenewal:          time.Now().AddDate(0, 1, 0).UTC(),
	}

	insertedSub, err := sf.Repo.UpsertSubscription(ctx, sub)
	assert.NoError(t, err)
	assert.NotNil(t, insertedSub)

	// Test Update Usage
	err = sf.Repo.UpdateUsage(ctx, "user-123")
	assert.NoError(t, err)

	updatedSub, err := sf.Repo.FindSubscriptionByUserID(ctx, "user-123")
	assert.NoError(t, err)
	assert.NotNil(t, updatedSub)
	assert.Equal(t, int32(9), updatedSub.UsageDesc)

	// Test Delete Subscription
	err = sf.Repo.DeleteSubscription(ctx, updatedSub.ID)
	assert.NoError(t, err)

	deletedSub, err := sf.Repo.FindSubscriptionByUserID(ctx, "user-123")
	assert.Error(t, err) // Should error because not found
	assert.Nil(t, deletedSub)
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
	require.NoError(t, err)

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
	require.NoError(t, err)

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
	require.NoError(t, err)

	t.Run("MySQL", func(t *testing.T) {
		testLibraryOperations(t, sfMysql)
	})
}
