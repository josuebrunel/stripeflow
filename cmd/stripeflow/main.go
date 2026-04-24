package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/josuebrunel/gopkg/xenv"
	"github.com/josuebrunel/stripeflow"
	"github.com/josuebrunel/stripeflow/migrations"

	// Drivers
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Config struct {
	DatabaseURL         string `env:"DATABASE_URL" required:"true"`
	StripeSecretKey     string `env:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret string `env:"WEBHOOK_SECRET"`
}

func main() {
	var (
		migrateFlag   = flag.String("migrate", "", "Run migrations (up or down)")
		provisionFlag = flag.String("provision", "", "Provision products from JSON file")
		syncFlag      = flag.Bool("sync", false, "Sync products from Stripe")
		deleteFlag    = flag.String("delete", "", "Delete a product by ID or 'all' for all products")
	)

	flag.Parse()

	if *migrateFlag == "" && *provisionFlag == "" && !*syncFlag && *deleteFlag == "" {
		fmt.Println("No command specified. Use -help for usage.")
		os.Exit(1)
	}

	var cfg Config
	// Try loading with prefix first
	err := xenv.LoadWithOptions(&cfg, xenv.Options{Prefix: "STRIPEFLOW_"})
	if err != nil {
		// Fallback to loading without prefix
		if err2 := xenv.Load(&cfg); err2 != nil {
			log.Fatalf("Failed to load environment variables: %v (also tried without STRIPEFLOW_ prefix: %v)", err, err2)
		}
	}

	dialect, dsn, err := parseDBURL(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Invalid database URL: %v", err)
	}

	db, err := sql.Open(dialect, dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	ctx := context.Background()

	// 1. Migrations
	if *migrateFlag != "" {
		switch *migrateFlag {
		case "up":
			if err := migrations.MigrateUp(db, dialect); err != nil {
				log.Fatalf("Migration up failed: %v", err)
			}
			log.Println("Migrations up applied successfully.")
		case "down":
			if err := migrations.MigrateDown(db, dialect); err != nil {
				log.Fatalf("Migration down failed: %v", err)
			}
			log.Println("Migrations down applied successfully.")
		default:
			log.Fatalf("Invalid migrate value: %q (must be 'up' or 'down')", *migrateFlag)
		}
	}

	// For commands that require stripeflow client
	if *provisionFlag != "" || *syncFlag || *deleteFlag != "" {
		if cfg.StripeSecretKey == "" {
			log.Fatal("Stripe secret key is required for Stripe operations (set STRIPEFLOW_STRIPE_SECRET_KEY)")
		}

		client, err := stripeflow.New(stripeflow.Config{
			Dialect:         stripeflow.Dialect(dialect),
			DB:              db,
			StripeSecretKey: cfg.StripeSecretKey,
			WebhookSecret:   cfg.StripeWebhookSecret, // not strictly required for these CLI commands but good to have
			GetUserID: func(r *http.Request) (string, error) {
				return "", nil // Not used in CLI context
			},
		})
		if err != nil {
			log.Fatalf("Failed to initialize stripeflow client: %v", err)
		}

		if *provisionFlag != "" {
			data, err := os.ReadFile(*provisionFlag)
			if err != nil {
				log.Fatalf("Failed to read provision file: %v", err)
			}
			results, err := client.ProvisionProductsFromJSON(ctx, data)
			if err != nil {
				log.Fatalf("Provisioning failed: %v", err)
			}

			totalPrices := 0
			for _, res := range results {
				totalPrices += len(res.Prices)
			}
			log.Printf("Provisioned %d products successfully with %d total prices.", len(results), totalPrices)
		}

		if *syncFlag {
			res, err := client.SyncProducts(ctx)
			if err != nil {
				log.Fatalf("Sync failed: %v", err)
			}

			log.Printf("Sync completed: %d products and %d prices upserted.", res.ProductsUpserted, res.PricesUpserted)
		}

		if *deleteFlag != "" {
			if *deleteFlag == "all" {
				if err := client.DeleteAllProducts(ctx); err != nil {
					log.Fatalf("Delete all products failed: %v", err)
				}
				log.Println("All products deleted successfully.")
			} else {
				if err := client.DeleteProduct(ctx, *deleteFlag); err != nil {
					log.Fatalf("Delete product %q failed: %v", *deleteFlag, err)
				}
				log.Printf("Product %q deleted successfully.", *deleteFlag)
			}
		}
	}
}

// parseDBURL parses a database URL like postgres://user:pass@host/db or sqlite://data.db
// and returns the driver name and the DSN string formatted for sql.Open.
func parseDBURL(url string) (string, string, error) {
	if strings.HasPrefix(url, "postgres://") || strings.HasPrefix(url, "postgresql://") {
		return "postgres", url, nil
	}
	if strings.HasPrefix(url, "sqlite://") {
		dsn := strings.TrimPrefix(url, "sqlite://")
		return "sqlite", dsn, nil
	}
	if strings.HasPrefix(url, "sqlite3://") {
		dsn := strings.TrimPrefix(url, "sqlite3://")
		return "sqlite", dsn, nil
	}
	// Fallback/guess based on driver format if no clear prefix is given
	if strings.Contains(url, "@tcp(") {
		return "mysql", strings.TrimPrefix(url, "mysql://"), nil
	}
	if strings.Contains(url, "user=") && strings.Contains(url, "dbname=") {
		return "postgres", url, nil
	}
	// Default to sqlite
	return "sqlite", url, nil
}
