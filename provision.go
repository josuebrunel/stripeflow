package stripeflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/stripe/stripe-go/v82"
	stripemeter "github.com/stripe/stripe-go/v82/billing/meter"
	stripeprice "github.com/stripe/stripe-go/v82/price"
	stripeproduct "github.com/stripe/stripe-go/v82/product"
)

// --------------------------------------------------------------------------
// Provision input types
// --------------------------------------------------------------------------

// ProvisionParams describes a complete product with all its prices to create
// in Stripe in a single call. This is designed for use cases like a CLI that
// reads a JSON file and provisions an entire product catalogue at once.
//
// Example usage with JSON:
//
//	raw, _ := os.ReadFile("product.json")
//	result, err := client.ProvisionProductFromJSON(ctx, raw)
//
// Example usage programmatically:
//
//	result, err := client.ProvisionProduct(ctx, stripeflow.ProvisionParams{
//	    Product: stripeflow.ProvisionProductParams{
//	        Name:        "My SaaS",
//	        Description: "A great product",
//	    },
//	    Prices: []stripeflow.ProvisionPriceParams{
//	        {
//	            Nickname:   "Monthly",
//	            Currency:   "usd",
//	            UnitAmount: 2999,
//	            Recurring:  &stripeflow.ProvisionRecurringParams{Interval: "month"},
//	        },
//	    },
//	})
type ProvisionParams struct {
	Product ProvisionProductParams `json:"product"`
	Prices  []ProvisionPriceParams `json:"prices"`
}

// ProvisionProductParams describes the product to create.
type ProvisionProductParams struct {
	// Name is the product name (required).
	Name string `json:"name"`
	// Description is an optional product description.
	Description string `json:"description,omitempty"`
	// Images are optional URLs to product images.
	Images []string `json:"images,omitempty"`
	// Metadata is optional key-value metadata attached to the product.
	Metadata map[string]string `json:"metadata,omitempty"`
	// MarketingFeatures lists feature bullet points shown on Stripe-hosted pages.
	MarketingFeatures []ProvisionFeature `json:"marketing_features,omitempty"`
}

// ProvisionFeature is a marketing feature displayed on Stripe-hosted surfaces.
type ProvisionFeature struct {
	Name string `json:"name"`
}

// ProvisionPriceParams describes a price to create for the product.
type ProvisionPriceParams struct {
	// Nickname is a human-readable label for the price (e.g. "Growth — monthly").
	Nickname string `json:"nickname,omitempty"`
	// Currency is a 3-letter ISO 4217 code, e.g. "usd" (required).
	Currency string `json:"currency"`
	// BillingScheme is "per_unit" (default) or "tiered".
	BillingScheme string `json:"billing_scheme,omitempty"`
	// UnitAmount is the price in the smallest currency unit (e.g. cents).
	UnitAmount int64 `json:"unit_amount"`
	// Recurring configures billing recurrence. Nil for one-time prices.
	Recurring *ProvisionRecurringParams `json:"recurring,omitempty"`
	// TransformQuantity configures billing per N units (e.g. per 1000 API calls).
	TransformQuantity *ProvisionTransformQtyParams `json:"transform_quantity,omitempty"`
	// Metadata is optional key-value metadata attached to the price.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ProvisionRecurringParams configures the billing cycle for a price.
type ProvisionRecurringParams struct {
	// Interval is "day", "week", "month", or "year" (required for recurring prices).
	Interval string `json:"interval"`
	// IntervalCount defaults to 1 (every interval).
	IntervalCount int64 `json:"interval_count,omitempty"`
	// UsageType is "licensed" (default) or "metered".
	UsageType string `json:"usage_type,omitempty"`
	// Meter is the ID of the meter tracking usage for metered prices (stripe-go v82+).
	// This replaces the legacy aggregate_usage field.
	Meter string `json:"meter,omitempty"`
	// AggregateUsage is accepted in JSON input for backward compatibility but
	// is no longer sent to Stripe in v82+. Use Meter instead for metered billing.
	AggregateUsage string `json:"aggregate_usage,omitempty"`
	// MeterEventName will auto-create a meter with this event name during provisioning.
	MeterEventName string `json:"meter_event_name,omitempty"`
	// MeterDisplayName is the display name for the auto-created meter.
	MeterDisplayName string `json:"meter_display_name,omitempty"`
}

// ProvisionTransformQtyParams configures billing per N units.
type ProvisionTransformQtyParams struct {
	// DivideBy is the divisor (e.g. 1000 to bill per 1000 units).
	DivideBy int64 `json:"divide_by"`
	// Round is "up" or "down".
	Round string `json:"round"`
}

// --------------------------------------------------------------------------
// Provision result types
// --------------------------------------------------------------------------

// ProvisionResult contains the IDs of all resources created by ProvisionProduct.
type ProvisionResult struct {
	ProductID string               `json:"product_id"`
	Prices    []ProvisionPriceInfo `json:"prices"`
}

// ProvisionPriceInfo describes a single price created during provisioning.
type ProvisionPriceInfo struct {
	PriceID  string `json:"price_id"`
	Nickname string `json:"nickname,omitempty"`
}

// --------------------------------------------------------------------------
// Provision functions
// --------------------------------------------------------------------------

// ProvisionProductsFromJSON is a convenience wrapper that unmarshals a JSON array
// of ProvisionParams and calls ProvisionProduct for each.
//
//	raw, _ := os.ReadFile("products.json")
//	results, err := client.ProvisionProductsFromJSON(ctx, raw)
func (c *Client) ProvisionProductsFromJSON(ctx context.Context, data []byte) ([]ProvisionResult, error) {
	var list []ProvisionParams

	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("stripeflow: invalid JSON input (must be an array of products): %w", err)
	}

	var results []ProvisionResult
	for _, params := range list {
		res, err := c.ProvisionProduct(ctx, params)
		if err != nil {
			return results, err
		}
		results = append(results, *res)
	}
	return results, nil
}

// ProvisionProduct creates a product and all its associated prices in Stripe,
// syncing each resource to the local database. The operation is sequential:
// the product is created first, then each price is created in order.
//
// If any price creation fails, the product and any previously created prices
// will remain in Stripe — check your Stripe dashboard to clean up.
func (c *Client) ProvisionProduct(ctx context.Context, params ProvisionParams) (*ProvisionResult, error) {
	if err := validateProvisionParams(params); err != nil {
		return nil, err
	}

	// Phase 1: Create the product in Stripe.
	slog.Info("stripeflow: provisioning product", "name", params.Product.Name)

	prodParams := &stripe.ProductParams{
		Name:        stripe.String(params.Product.Name),
		Description: stripe.String(params.Product.Description),
	}
	for _, img := range params.Product.Images {
		prodParams.Images = append(prodParams.Images, stripe.String(img))
	}
	for k, v := range params.Product.Metadata {
		prodParams.AddMetadata(k, v)
	}
	for _, f := range params.Product.MarketingFeatures {
		prodParams.MarketingFeatures = append(prodParams.MarketingFeatures,
			&stripe.ProductMarketingFeatureParams{
				Name: stripe.String(f.Name),
			},
		)
	}

	prod, err := stripeproduct.New(prodParams)
	if err != nil {
		return nil, fmt.Errorf("stripeflow: stripe create product: %w", err)
	}

	// Sync product locally.
	local := Product{
		ID:          prod.ID,
		Name:        prod.Name,
		Description: prod.Description,
		Active:      prod.Active,
	}
	if prod.Created != 0 {
		t := time.Unix(prod.Created, 0)
		local.StripeCreatedAt = &t
	}
	if err := c.repo.upsertProduct(ctx, local); err != nil {
		return nil, fmt.Errorf("stripeflow: store product: %w", err)
	}

	slog.Info("stripeflow: product created", "product_id", prod.ID)

	// Phase 2: Create each price.
	result := &ProvisionResult{ProductID: prod.ID}

	for i, pi := range params.Prices {
		slog.Info("stripeflow: creating price", "index", i, "nickname", pi.Nickname)

		priceParams := &stripe.PriceParams{
			Product:    stripe.String(prod.ID),
			Currency:   stripe.String(pi.Currency),
			UnitAmount: stripe.Int64(pi.UnitAmount),
		}

		if pi.Nickname != "" {
			priceParams.Nickname = stripe.String(pi.Nickname)
		}
		if pi.BillingScheme != "" {
			priceParams.BillingScheme = stripe.String(pi.BillingScheme)
		}

		// Recurring configuration.
		if pi.Recurring != nil {
			priceParams.Recurring = &stripe.PriceRecurringParams{
				Interval: stripe.String(pi.Recurring.Interval),
			}
			if pi.Recurring.IntervalCount > 0 {
				priceParams.Recurring.IntervalCount = stripe.Int64(pi.Recurring.IntervalCount)
			}

			usageType := pi.Recurring.UsageType
			if usageType == "" {
				usageType = "licensed"
			}
			priceParams.Recurring.UsageType = stripe.String(usageType)

			if usageType == "metered" {
				if pi.Recurring.MeterEventName != "" {
					displayName := pi.Recurring.MeterDisplayName
					if displayName == "" {
						displayName = pi.Recurring.MeterEventName
					}
					m, err := stripemeter.New(&stripe.BillingMeterParams{
						DisplayName: stripe.String(displayName),
						EventName:   stripe.String(pi.Recurring.MeterEventName),
						DefaultAggregation: &stripe.BillingMeterDefaultAggregationParams{
							Formula: stripe.String("count"),
						},
					})
					if err != nil {
						return result, fmt.Errorf("stripeflow: failed to create meter %q: %w", pi.Recurring.MeterEventName, err)
					}
					priceParams.Recurring.Meter = stripe.String(m.ID)
					slog.Info("stripeflow: meter auto-created", "meter_id", m.ID, "event_name", pi.Recurring.MeterEventName)
				} else if pi.Recurring.Meter != "" {
					priceParams.Recurring.Meter = stripe.String(pi.Recurring.Meter)
				}
			}
		}

		// Transform quantity (e.g. bill per 1000 API calls).
		if pi.TransformQuantity != nil {
			priceParams.TransformQuantity = &stripe.PriceTransformQuantityParams{
				DivideBy: stripe.Int64(pi.TransformQuantity.DivideBy),
				Round:    stripe.String(pi.TransformQuantity.Round),
			}
		}

		for k, v := range pi.Metadata {
			priceParams.AddMetadata(k, v)
		}

		price, err := stripeprice.New(priceParams)
		if err != nil {
			return result, fmt.Errorf("stripeflow: stripe create price %q (index %d): %w", pi.Nickname, i, err)
		}

		// Sync price locally.
		lp := Price{
			ID:        price.ID,
			ProductID: prod.ID,
			Currency:  string(price.Currency),
			Active:    price.Active,
		}
		if price.UnitAmount != 0 {
			ua := price.UnitAmount
			lp.UnitAmount = &ua
		}
		if price.Recurring != nil {
			lp.RecurringInterval = string(price.Recurring.Interval)
			count := int(price.Recurring.IntervalCount)
			lp.RecurringCount = &count
		}
		if price.Created != 0 {
			t := time.Unix(price.Created, 0)
			lp.StripeCreatedAt = &t
		}
		if err := c.repo.upsertPrice(ctx, lp); err != nil {
			return result, fmt.Errorf("stripeflow: store price %s: %w", price.ID, err)
		}

		slog.Info("stripeflow: price created", "price_id", price.ID, "nickname", pi.Nickname)

		result.Prices = append(result.Prices, ProvisionPriceInfo{
			PriceID:  price.ID,
			Nickname: pi.Nickname,
		})
	}

	slog.Info("stripeflow: provisioning complete",
		"product_id", result.ProductID,
		"prices_created", len(result.Prices),
	)
	return result, nil
}

// --------------------------------------------------------------------------
// Validation
// --------------------------------------------------------------------------

func validateProvisionParams(p ProvisionParams) error {
	if p.Product.Name == "" {
		return fmt.Errorf("stripeflow: product name is required")
	}
	if len(p.Prices) == 0 {
		return fmt.Errorf("stripeflow: at least one price is required")
	}
	for i, price := range p.Prices {
		if price.Currency == "" {
			return fmt.Errorf("stripeflow: prices[%d]: currency is required", i)
		}
		if price.Recurring != nil && price.Recurring.Interval == "" {
			return fmt.Errorf("stripeflow: prices[%d] %q: recurring.interval is required", i, price.Nickname)
		}
	}
	return nil
}
