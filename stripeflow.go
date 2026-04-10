package stripeflow

import (
	"database/sql"
	"fmt"
	"stripeflow/handler"
	"stripeflow/repository"
	"stripeflow/repository/mysql"
	"stripeflow/repository/postgres"
	"stripeflow/repository/sqlite"
	"stripeflow/service"
)

type Config struct {
	Dialect         string
	DB              *sql.DB
	StripeSecretKey string
	WebhookSecret   string
	RedirectURL     string
}

type StripeFlow struct {
	Repo         repository.Querier
	Service      *service.Service
	Handler      *handler.Handler
}

func New(cfg Config, userResolver handler.UserDetailsResolver) (*StripeFlow, error) {
	var repo repository.Querier
	switch cfg.Dialect {
	case "postgres":
		repo = postgres.New(cfg.DB)
	case "mysql":
		repo = mysql.New(cfg.DB)
	case "sqlite", "sqlite3":
		repo = sqlite.New(cfg.DB)
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", cfg.Dialect)
	}

	svcCfg := service.LoadConfig()
	if cfg.StripeSecretKey != "" {
		svcCfg.StripeSecretKey = cfg.StripeSecretKey
	}
	if cfg.WebhookSecret != "" {
		svcCfg.StripeWebhookSecret = cfg.WebhookSecret
	}
	if cfg.RedirectURL != "" {
		svcCfg.CheckoutRedirect = cfg.RedirectURL
	}

	svc := service.New(repo, svcCfg)
	h := handler.New(repo, svc, svcCfg, userResolver)

	return &StripeFlow{
		Repo:    repo,
		Service: svc,
		Handler: h,
	}, nil
}
