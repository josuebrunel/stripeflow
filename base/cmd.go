package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"propcopyai/pkg/db/models"
	"propcopyai/pkg/util"
	"sync"

	"github.com/aarondl/opt/omit"
	"github.com/aarondl/opt/omitnull"
	"github.com/josuebrunel/gopkg/xlog"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/types"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/price"
)

func SyncPrices(ctx context.Context, rdb *sql.DB) error {
	db := bob.NewDB(rdb)
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
	params := &stripe.PriceListParams{} // Active: stripe.Bool(true)}
	params.AddExpand("data.product")
	params.Limit = stripe.Int64(5)
	result := price.List(params)
	var wg sync.WaitGroup
	for _, p := range result.PriceList().Data {
		wg.Add(1)
		go func(p *stripe.Price) {
			defer wg.Done()
			xlog.Info("Syncing plan", "plan", p.ID)
			xlog.Debug("Plan from price", "stripe_price", p)
			var (
				sortOrder       = util.AtoI32(p.Metadata["sort_order"])
				maxDescriptions = util.AtoI32(p.Metadata["max_descriptions"])
				maxPhotos       = util.AtoI32(p.Metadata["max_photos"])
				billingCycle    = getBillingCycle(p)
			)

			var features json.RawMessage
			if err := features.UnmarshalJSON([]byte(p.Metadata["features"])); err != nil {
				xlog.Error("Error unmarshalling features", "error", err)
			}
			query := models.Plans.Insert(&models.PlanSetter{
				Name:            omit.From(p.Product.Name),
				Slug:            omit.From(util.Slugify(p.Product.Name)),
				StripeProductID: omit.From(p.Product.ID),
				StripePriceID:   omit.From(p.ID),
				Description:     omitnull.From(p.Product.Description),
				PriceUsd:        omit.From(int32(p.UnitAmount)),
				IsActive:        omitnull.From(p.Active),
				BillingCycle:    omit.From(billingCycle),
				Features:        omitnull.From(types.NewJSON(features)),
				SortOrder:       omitnull.From(sortOrder),
				MaxDescriptions: omitnull.From(maxDescriptions),
				MaxPhotos:       omitnull.From(maxPhotos),
			}, im.OnConflict("stripe_price_id").DoUpdate(
				im.SetExcluded("name"),
				im.SetExcluded("slug"),
				im.SetExcluded("stripe_product_id"),
				im.SetExcluded("description"),
				im.SetExcluded("price_usd"),
				im.SetExcluded("is_active"),
				im.SetExcluded("billing_cycle"),
				im.SetExcluded("features"),
				im.SetExcluded("sort_order"),
				im.SetExcluded("max_descriptions"),
				im.SetExcluded("max_photos"),
			))
			plan, err := query.One(ctx, db)
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

func getBillingCycle(p *stripe.Price) string {
	if p.Recurring == nil {
		return "month"
	}
	return string(p.Recurring.Interval)
}
