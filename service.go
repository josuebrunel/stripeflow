package stripeflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/stripe/stripe-go/v82"
	stripeprice "github.com/stripe/stripe-go/v82/price"
	stripeproduct "github.com/stripe/stripe-go/v82/product"
)

// --------------------------------------------------------------------------
// Product management
// --------------------------------------------------------------------------

// CreateProductParams defines a new product to create in Stripe (and sync locally).
type CreateProductParams struct {
	Name        string
	Description string
	// Images are URLs to product images.
	Images   []string
	Metadata map[string]string
}

// CreateProduct creates a product in Stripe and stores it locally.
func (c *Client) CreateProduct(ctx context.Context, p CreateProductParams) (*Product, error) {
	if p.Name == "" {
		return nil, fmt.Errorf("stripeflow: product Name is required")
	}

	params := &stripe.ProductParams{
		Name:        stripe.String(p.Name),
		Description: stripe.String(p.Description),
	}
	for _, img := range p.Images {
		params.Images = append(params.Images, stripe.String(img))
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}

	prod, err := stripeproduct.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripeflow: stripe create product: %w", err)
	}

	local := Product{
		ID:          prod.ID,
		Name:        prod.Name,
		Description: prod.Description,
		Active:      prod.Active,
	}
	if len(prod.Metadata) > 0 {
		b, _ := json.Marshal(prod.Metadata)
		m := json.RawMessage(b)
		local.Metadata = &m
	}
	if len(prod.MarketingFeatures) > 0 {
		b, _ := json.Marshal(prod.MarketingFeatures)
		f := json.RawMessage(b)
		local.Features = &f
	}
	if prod.Created != 0 {
		t := time.Unix(prod.Created, 0)
		local.StripeCreatedAt = &t
	}

	if err := c.repo.upsertProduct(ctx, local); err != nil {
		return nil, fmt.Errorf("stripeflow: store product: %w", err)
	}
	return &local, nil
}

// UpdateProductParams describes editable fields on an existing product.
type UpdateProductParams struct {
	// StripeProductID is the Stripe product ID (prod_...).
	StripeProductID string
	Name            *string
	Description     *string
	Active          *bool
	Metadata        map[string]string
}

// UpdateProduct updates a product in Stripe and refreshes the local copy.
func (c *Client) UpdateProduct(ctx context.Context, p UpdateProductParams) (*Product, error) {
	if p.StripeProductID == "" {
		return nil, fmt.Errorf("stripeflow: StripeProductID is required")
	}

	params := &stripe.ProductParams{}
	if p.Name != nil {
		params.Name = p.Name
	}
	if p.Description != nil {
		params.Description = p.Description
	}
	if p.Active != nil {
		params.Active = p.Active
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}

	prod, err := stripeproduct.Update(p.StripeProductID, params)
	if err != nil {
		return nil, fmt.Errorf("stripeflow: stripe update product: %w", err)
	}

	local := Product{
		ID:          prod.ID,
		Name:        prod.Name,
		Description: prod.Description,
		Active:      prod.Active,
	}
	if len(prod.Metadata) > 0 {
		b, _ := json.Marshal(prod.Metadata)
		m := json.RawMessage(b)
		local.Metadata = &m
	}
	if len(prod.MarketingFeatures) > 0 {
		b, _ := json.Marshal(prod.MarketingFeatures)
		f := json.RawMessage(b)
		local.Features = &f
	}
	if prod.Created != 0 {
		t := time.Unix(prod.Created, 0)
		local.StripeCreatedAt = &t
	}
	if err := c.repo.upsertProduct(ctx, local); err != nil {
		return nil, fmt.Errorf("stripeflow: store updated product: %w", err)
	}
	return &local, nil
}

// --------------------------------------------------------------------------
// Price management
// --------------------------------------------------------------------------

// PriceInterval represents billing recurrence.
type PriceInterval string

const (
	IntervalDay   PriceInterval = "day"
	IntervalWeek  PriceInterval = "week"
	IntervalMonth PriceInterval = "month"
	IntervalYear  PriceInterval = "year"
)

// CreatePriceParams defines a new recurring or one-time price.
type CreatePriceParams struct {
	// StripeProductID is the parent product (prod_...).
	StripeProductID string
	// UnitAmount is in the smallest currency unit (e.g. cents for USD).
	UnitAmount int64
	// Currency is a 3-letter ISO code, e.g. "usd".
	Currency string
	// Recurring – if nil, a one-time price is created.
	Recurring *RecurringParams
	Metadata  map[string]string
}

// RecurringParams configures the billing cycle for a price.
type RecurringParams struct {
	Interval      PriceInterval
	IntervalCount int64 // 1 = every interval, 3 = every 3 intervals, etc.
}

// CreatePrice creates a price in Stripe and stores it locally.
func (c *Client) CreatePrice(ctx context.Context, p CreatePriceParams) (*Price, error) {
	if p.StripeProductID == "" {
		return nil, fmt.Errorf("stripeflow: StripeProductID is required")
	}
	if p.Currency == "" {
		return nil, fmt.Errorf("stripeflow: Currency is required")
	}

	params := &stripe.PriceParams{
		Product:    stripe.String(p.StripeProductID),
		UnitAmount: stripe.Int64(p.UnitAmount),
		Currency:   stripe.String(p.Currency),
	}
	if p.Recurring != nil {
		count := p.Recurring.IntervalCount
		if count <= 0 {
			count = 1
		}
		params.Recurring = &stripe.PriceRecurringParams{
			Interval:      stripe.String(string(p.Recurring.Interval)),
			IntervalCount: stripe.Int64(count),
		}
	}
	for k, v := range p.Metadata {
		params.AddMetadata(k, v)
	}

	price, err := stripeprice.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripeflow: stripe create price: %w", err)
	}

	local := Price{
		ID:        price.ID,
		ProductID: p.StripeProductID,
		Currency:  string(price.Currency),
		Active:    price.Active,
	}
	if len(price.Metadata) > 0 {
		b, _ := json.Marshal(price.Metadata)
		m := json.RawMessage(b)
		local.Metadata = &m
	}
	if price.UnitAmount != 0 {
		ua := price.UnitAmount
		local.UnitAmount = &ua
	}
	if price.Recurring != nil {
		local.RecurringInterval = string(price.Recurring.Interval)
		count := int(price.Recurring.IntervalCount)
		local.RecurringCount = &count
	}
	if price.Created != 0 {
		t := time.Unix(price.Created, 0)
		local.StripeCreatedAt = &t
	}

	if err := c.repo.upsertPrice(ctx, local); err != nil {
		return nil, fmt.Errorf("stripeflow: store price: %w", err)
	}
	return &local, nil
}

// ArchivePrice marks a price as inactive in Stripe (prices cannot be deleted).
func (c *Client) ArchivePrice(ctx context.Context, priceID string) error {
	_, err := stripeprice.Update(priceID, &stripe.PriceParams{
		Active: stripe.Bool(false),
	})
	return err
}

// --------------------------------------------------------------------------
// Sync
// --------------------------------------------------------------------------

// SyncResult summarises a full catalogue synchronisation.
type SyncResult struct {
	ProductsUpserted int
	PricesUpserted   int
}

// SyncProducts fetches all products and their prices from Stripe and upserts
// them into the local database. Call this on startup or via a cron job.
func (c *Client) SyncProducts(ctx context.Context) (*SyncResult, error) {
	result := &SyncResult{}

	prodIter := stripeproduct.List(&stripe.ProductListParams{
		ListParams: stripe.ListParams{Context: ctx},
	})
	for prodIter.Next() {
		prod := prodIter.Current().(*stripe.Product)
		var createdAt *time.Time
		if prod.Created != 0 {
			t := time.Unix(prod.Created, 0)
			createdAt = &t
		}
		local := Product{
			ID:              prod.ID,
			Name:            prod.Name,
			Description:     prod.Description,
			Active:          prod.Active,
			StripeCreatedAt: createdAt,
		}
		if len(prod.Metadata) > 0 {
			b, _ := json.Marshal(prod.Metadata)
			m := json.RawMessage(b)
			local.Metadata = &m
		}
		if len(prod.MarketingFeatures) > 0 {
			b, _ := json.Marshal(prod.MarketingFeatures)
			f := json.RawMessage(b)
			local.Features = &f
		}
		if err := c.repo.upsertProduct(ctx, local); err != nil {
			return result, fmt.Errorf("stripeflow: sync product %s: %w", prod.ID, err)
		}
		result.ProductsUpserted++
	}
	if err := prodIter.Err(); err != nil {
		return result, fmt.Errorf("stripeflow: list products: %w", err)
	}

	priceIter := stripeprice.List(&stripe.PriceListParams{
		ListParams: stripe.ListParams{Context: ctx},
	})
	for priceIter.Next() {
		price := priceIter.Current().(*stripe.Price)
		if price.Product == nil {
			continue
		}
		var createdAt *time.Time
		if price.Created != 0 {
			t := time.Unix(price.Created, 0)
			createdAt = &t
		}
		lp := Price{
			ID:              price.ID,
			ProductID:       price.Product.ID,
			Currency:        string(price.Currency),
			Active:          price.Active,
			StripeCreatedAt: createdAt,
		}
		if len(price.Metadata) > 0 {
			b, _ := json.Marshal(price.Metadata)
			m := json.RawMessage(b)
			lp.Metadata = &m
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
		if err := c.repo.upsertPrice(ctx, lp); err != nil {
			return result, fmt.Errorf("stripeflow: sync price %s: %w", price.ID, err)
		}
		result.PricesUpserted++
	}
	if err := priceIter.Err(); err != nil {
		return result, fmt.Errorf("stripeflow: list prices: %w", err)
	}

	slog.Info("stripeflow: sync complete",
		"products", result.ProductsUpserted, "prices", result.PricesUpserted)
	return result, nil
}

// --------------------------------------------------------------------------
// Query helpers
// --------------------------------------------------------------------------

// ListProducts returns locally cached products.
func (c *Client) ListProducts(ctx context.Context, activeOnly bool) ([]Product, error) {
	return c.repo.listProducts(ctx, activeOnly)
}

// ListPrices returns locally cached prices for a product.
func (c *Client) ListPrices(ctx context.Context, productID string) ([]Price, error) {
	return c.repo.listPricesForProduct(ctx, productID)
}

// GetSubscription retrieves the current subscription state for a user.
func (c *Client) GetSubscription(ctx context.Context, userID string) (*Subscription, error) {
	return c.repo.getSubscriptionByUserID(ctx, userID)
}

// GetSubscriptionByID retrieves a subscription by its primary key ID.
func (c *Client) GetSubscriptionByID(ctx context.Context, id int64) (*Subscription, error) {
	return c.repo.getSubscriptionByID(ctx, id)
}

// GetSubscriptionByCustomerID retrieves a subscription by Stripe Customer ID.
func (c *Client) GetSubscriptionByCustomerID(ctx context.Context, customerID string) (*Subscription, error) {
	return c.repo.getSubscriptionByCustomerID(ctx, customerID)
}

// GetSubscriptionByStripeSubID retrieves a subscription by Stripe Subscription ID.
func (c *Client) GetSubscriptionByStripeSubID(ctx context.Context, subID string) (*Subscription, error) {
	return c.repo.getSubscriptionByStripeSubID(ctx, subID)
}

// GetProductByID retrieves a product by its ID.
func (c *Client) GetProductByID(ctx context.Context, id string) (*Product, error) {
	return c.repo.getProductByID(ctx, id)
}

// DeleteProduct deletes a product and all of its associated prices from the local database.
// In Stripe, prices are archived, and the product itself is archived (made inactive) because
// Stripe does not allow deleting products that have ever had prices.
func (c *Client) DeleteProduct(ctx context.Context, productID string) error {
	// First fetch all prices to archive them
	prices, err := c.repo.listPricesForProduct(ctx, productID)
	if err != nil {
		return fmt.Errorf("stripeflow: list prices for product: %w", err)
	}

	for _, p := range prices {
		_ = c.ArchivePrice(ctx, p.ID) // Best effort to archive price in Stripe
	}

	// Archive product in Stripe (since it cannot be deleted if it has prices)
	_, _ = stripeproduct.Update(productID, &stripe.ProductParams{
		Active: stripe.Bool(false),
	})

	// Delete locally
	if err := c.repo.deleteProduct(ctx, productID); err != nil {
		return fmt.Errorf("stripeflow: local delete product: %w", err)
	}

	slog.Info("stripeflow: product and prices deleted locally, archived in Stripe", "product_id", productID)
	return nil
}

// DeleteAllProducts deletes all products and prices from the local database
// and attempts to archive them in Stripe.
func (c *Client) DeleteAllProducts(ctx context.Context) error {
	products, err := c.repo.listProducts(ctx, false)
	if err != nil {
		return fmt.Errorf("stripeflow: list all products: %w", err)
	}

	for _, prod := range products {
		_ = c.DeleteProduct(ctx, prod.ID) // Best effort to archive in Stripe and delete locally
	}

	// Failsafe clean up any remaining local records
	if err := c.repo.deleteAllProducts(ctx); err != nil {
		return fmt.Errorf("stripeflow: local delete all products: %w", err)
	}

	slog.Info("stripeflow: all products and prices deleted locally")
	return nil
}
