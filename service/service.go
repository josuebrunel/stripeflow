package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"stripeflow/db/models"
	"stripeflow/repository"
	"sync"
	"time"

	"github.com/josuebrunel/gopkg/xenv"
	"github.com/josuebrunel/gopkg/xlog"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/price"
	stSubscription "github.com/stripe/stripe-go/v82/subscription"
)

type Config struct {
	StripeSecretKey     string `env:"STRIPEFLOW_STRIPE_SECRET_KEY"`
	StripeWebhookSecret string `env:"STRIPEFLOW_STRIPE_WEBHOOK_SECRET"`
	CheckoutRedirect    string `env:"STRIPEFLOW_CHECKOUT_REDIRECT"`
}

func LoadConfig() Config {
	var cfg Config
	_ = xenv.Load(&cfg)
	if cfg.StripeSecretKey == "" {
		cfg.StripeSecretKey = os.Getenv("STRIPEFLOW_STRIPE_SECRET_KEY")
	}
	if cfg.StripeWebhookSecret == "" {
		cfg.StripeWebhookSecret = os.Getenv("STRIPEFLOW_STRIPE_WEBHOOK_SECRET")
	}
	if cfg.CheckoutRedirect == "" {
		cfg.CheckoutRedirect = os.Getenv("STRIPEFLOW_CHECKOUT_REDIRECT")
	}
	return cfg
}

type Service struct {
	repo repository.Querier
	cfg  Config
}

func New(repo repository.Querier, cfg Config) *Service {
	return &Service{
		repo: repo,
		cfg:  cfg,
	}
}

func (s *Service) SyncPrices(ctx context.Context) error {
	stripe.Key = s.cfg.StripeSecretKey
	params := &stripe.PriceListParams{}
	params.AddExpand("data.product")
	params.Limit = stripe.Int64(5) // Adjust limit as needed
	result := price.List(params)

	var wg sync.WaitGroup
	for _, p := range result.PriceList().Data {
		wg.Add(1)
		go func(p *stripe.Price) {
			defer wg.Done()
			xlog.Info("Syncing plan", "plan", p.ID)

			sortOrder, _ := strconv.Atoi(p.Metadata["sort_order"])
			maxDescriptions, _ := strconv.Atoi(p.Metadata["max_descriptions"])
			maxPhotos, _ := strconv.Atoi(p.Metadata["max_photos"])
			billingCycle := getBillingCycle(p)

			var features json.RawMessage
			if err := features.UnmarshalJSON([]byte(p.Metadata["features"])); err != nil {
				xlog.Error("Error unmarshalling features", "error", err)
			}

			plan := &models.Plan{
				Name:            p.Product.Name,
				Slug:            slugifyStr(p.Product.Name),
				StripeProductID: p.Product.ID,
				StripePriceID:   p.ID,
				PriceUsd:        int32(p.UnitAmount),
				IsActive:        p.Active,
				BillingCycle:    billingCycle,
				Features:        &features,
				SortOrder:       int32(sortOrder),
				MaxDescriptions: int32(maxDescriptions),
				MaxPhotos:       int32(maxPhotos),
			}
			if p.Product.Description != "" {
				plan.Description = &p.Product.Description
			}

			_, err := s.repo.UpsertPlan(ctx, plan)
			if err != nil {
				xlog.Error("Error inserting plan", "error", err)
				return
			}
			xlog.Info("Plan synced", "plan", plan.StripePriceID)
		}(p)
	}
	wg.Wait()
	return nil
}

func (s *Service) HandleInvoicePaid(ctx context.Context, invoice *stripe.Invoice, userID string) error {
	stripe.Key = s.cfg.StripeSecretKey
	var stSubID string
	if invoice.Lines != nil && len(invoice.Lines.Data) > 0 {
		for _, item := range invoice.Lines.Data {
			if item.Subscription != nil && item.Subscription.ID != "" {
				stSubID = item.Subscription.ID
				break
			}
		}
	}

	if stSubID == "" {
		return fmt.Errorf("no subscription found in invoice")
	}

	stSub, err := stSubscription.Get(stSubID, nil)
	if err != nil {
		return fmt.Errorf("error getting subscription from stripe: %w", err)
	}

	if len(stSub.Items.Data) == 0 {
		return fmt.Errorf("no items in stripe subscription %s", stSubID)
	}

	stItem := stSub.Items.Data[len(stSub.Items.Data)-1]
	stPrice := stItem.Price
	dateStart := stItem.CurrentPeriodStart
	dateEnd := stItem.CurrentPeriodEnd

	plan, err := s.repo.FindPlan(ctx, stPrice.ID)
	if err != nil {
		return fmt.Errorf("error finding plan %s: %w", stPrice.ID, err)
	}

	sub := &models.Subscription{
		StripeCustomerID:     invoice.Customer.ID,
		StripeSubscriptionID: stSubID,
		StripePriceID:        stPrice.ID,
		UserID:               userID,
		PlanName:             plan.Name,
		Status:               "active",
		UsageDesc:            plan.MaxDescriptions,
		UsagePhotos:          plan.MaxPhotos,
		DateStart:            time.Unix(dateStart, 0).UTC(),
		DateEnd:              time.Unix(dateEnd, 0).UTC(),
		DateRenewal:          time.Unix(dateEnd, 0).UTC(),
	}

	_, err = s.repo.UpsertSubscription(ctx, sub)
	if err != nil {
		return fmt.Errorf("error inserting user subscription: %w", err)
	}

	xlog.Info("User subscription created/updated", "subscription_id", stSubID, "user_id", userID)
	return nil
}

func (s *Service) HandleSubscriptionDeleted(ctx context.Context, sub *stripe.Subscription) error {
	dbSub, err := s.repo.FindSubscriptionByStripeID(ctx, sub.ID, sub.Customer.ID)
	if err != nil {
		return fmt.Errorf("error finding subscription %s: %w", sub.ID, err)
	}
	if dbSub == nil {
		return nil
	}

	err = s.repo.DeleteSubscription(ctx, dbSub.ID)
	if err != nil {
		return fmt.Errorf("error deleting subscription %s: %w", dbSub.ID, err)
	}

	xlog.Info("Subscription deleted", "subscription", sub.ID, "customer", sub.Customer.ID)
	return nil
}

func getBillingCycle(p *stripe.Price) string {
	if p.Recurring == nil {
		return "month"
	}
	return string(p.Recurring.Interval)
}
