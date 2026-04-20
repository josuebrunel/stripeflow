package stripeflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/price"
	stSubscription "github.com/stripe/stripe-go/v82/subscription"
)

// SyncPrices fetches all active prices from Stripe and upserts them as local plans.
func (sf *StripeFlow) SyncPrices(ctx context.Context) error {
	params := &stripe.PriceListParams{}
	params.AddExpand("data.product")
	params.Limit = stripe.Int64(100)
	result := price.List(params)

	var wg sync.WaitGroup
	for _, p := range result.PriceList().Data {
		wg.Add(1)
		go func(p *stripe.Price) {
			defer wg.Done()

			sortOrder := 0
			if v, ok := p.Metadata["sort_order"]; ok {
				fmt.Sscanf(v, "%d", &sortOrder)
			}

			billingCycle := "month"
			if p.Recurring != nil {
				billingCycle = string(p.Recurring.Interval)
			}

			var features json.RawMessage
			if raw, ok := p.Metadata["features"]; ok {
				if json.Valid([]byte(raw)) {
					features = json.RawMessage(raw)
				}
			}

			metadataBytes, _ := json.Marshal(p.Metadata)
			metadata := json.RawMessage(metadataBytes)

			plan := &Plan{
				Name:            p.Product.Name,
				Slug:            slugify(p.Product.Name),
				StripeProductID: p.Product.ID,
				StripePriceID:   p.ID,
				PriceUsd:        int32(p.UnitAmount),
				IsActive:        p.Active,
				BillingCycle:    billingCycle,
				Features:        &features,
				SortOrder:       int32(sortOrder),
				Metadata:        &metadata,
			}
			if p.Product.Description != "" {
				plan.Description = &p.Product.Description
			}

			if _, err := sf.repo.upsertPlan(ctx, plan); err != nil {
				slog.Error("sync: failed to upsert plan", "price_id", p.ID, "error", err)
				return
			}
			slog.Info("sync: plan synced", "price_id", p.ID)
		}(p)
	}
	wg.Wait()
	return nil
}

func (sf *StripeFlow) handleInvoicePaid(ctx context.Context, invoice *stripe.Invoice, userID string) error {
	var stSubID string
	if invoice.Lines != nil {
		for _, item := range invoice.Lines.Data {
			if item.Subscription != nil && item.Subscription.ID != "" {
				stSubID = item.Subscription.ID
				break
			}
		}
	}

	if stSubID == "" {
		return fmt.Errorf("no subscription found in invoice %s", invoice.ID)
	}

	stSub, err := stSubscription.Get(stSubID, nil)
	if err != nil {
		return fmt.Errorf("stripe: get subscription %s: %w", stSubID, err)
	}

	if len(stSub.Items.Data) == 0 {
		return fmt.Errorf("stripe: no items in subscription %s", stSubID)
	}

	stItem := stSub.Items.Data[len(stSub.Items.Data)-1]
	stPrice := stItem.Price

	plan, err := sf.repo.findPlan(ctx, stPrice.ID)
	if err != nil {
		return fmt.Errorf("plan not found for price %s: %w", stPrice.ID, err)
	}

	metadataBytes, _ := json.Marshal(stSub.Metadata)
	metadata := json.RawMessage(metadataBytes)

	sub := &Subscription{
		StripeCustomerID:     invoice.Customer.ID,
		StripeSubscriptionID: stSubID,
		StripePriceID:        stPrice.ID,
		UserID:               userID,
		PlanName:             plan.Name,
		Status:               string(stSub.Status),
		Metadata:             &metadata,
		DateStart:            time.Unix(stItem.CurrentPeriodStart, 0).UTC(),
		DateEnd:              time.Unix(stItem.CurrentPeriodEnd, 0).UTC(),
		DateRenewal:          time.Unix(stItem.CurrentPeriodEnd, 0).UTC(),
	}

	if _, err := sf.repo.upsertSubscription(ctx, sub); err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}

	slog.Info("invoice paid: subscription created/updated", "subscription_id", stSubID, "user_id", userID)
	return nil
}

func (sf *StripeFlow) handleSubscriptionUpdated(ctx context.Context, stSub *stripe.Subscription) error {
	dbSub, err := sf.repo.findSubscriptionByStripeID(ctx, stSub.ID, stSub.Customer.ID)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return fmt.Errorf("find subscription %s: %w", stSub.ID, err)
	}

	if len(stSub.Items.Data) == 0 {
		return fmt.Errorf("stripe: no items in subscription %s", stSub.ID)
	}

	stItem := stSub.Items.Data[len(stSub.Items.Data)-1]
	stPrice := stItem.Price

	plan, err := sf.repo.findPlan(ctx, stPrice.ID)
	if err != nil {
		return fmt.Errorf("plan not found for price %s: %w", stPrice.ID, err)
	}

	var userID string
	if dbSub != nil {
		userID = dbSub.UserID
	} else if stSub.Metadata != nil {
		userID = stSub.Metadata["user_id"]
	}

	if userID == "" {
		return fmt.Errorf("no user_id found for subscription %s", stSub.ID)
	}

	metadataBytes, _ := json.Marshal(stSub.Metadata)
	metadata := json.RawMessage(metadataBytes)

	sub := &Subscription{
		StripeCustomerID:     stSub.Customer.ID,
		StripeSubscriptionID: stSub.ID,
		StripePriceID:        stPrice.ID,
		UserID:               userID,
		PlanName:             plan.Name,
		Status:               string(stSub.Status),
		Metadata:             &metadata,
		DateStart:            time.Unix(stItem.CurrentPeriodStart, 0).UTC(),
		DateEnd:              time.Unix(stItem.CurrentPeriodEnd, 0).UTC(),
		DateRenewal:          time.Unix(stItem.CurrentPeriodEnd, 0).UTC(),
	}

	if _, err := sf.repo.upsertSubscription(ctx, sub); err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}

	slog.Info("subscription updated", "subscription_id", stSub.ID, "user_id", userID)
	return nil
}

func (sf *StripeFlow) handleSubscriptionDeleted(ctx context.Context, stSub *stripe.Subscription) error {
	dbSub, err := sf.repo.findSubscriptionByStripeID(ctx, stSub.ID, stSub.Customer.ID)
	if err != nil {
		return fmt.Errorf("find subscription %s: %w", stSub.ID, err)
	}
	if dbSub == nil {
		return nil
	}

	if err := sf.repo.deleteSubscription(ctx, dbSub.ID); err != nil {
		return fmt.Errorf("delete subscription %s: %w", dbSub.ID, err)
	}

	slog.Info("subscription deleted", "subscription_id", stSub.ID, "customer_id", stSub.Customer.ID)
	return nil
}
