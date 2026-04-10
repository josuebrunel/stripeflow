package postgres

import (
	"context"
	"stripeflow/db/models"
	"time"

	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/scan"
)

func UpsertPlanQuery(plan *models.Plan) bob.Query {
	return psql.Insert(
		im.Into("stripeflow_plans", "name", "slug", "stripe_product_id", "stripe_price_id", "description", "price_usd", "is_active", "billing_cycle", "features", "sort_order", "max_descriptions", "max_photos"),
		im.Values(psql.Arg(plan.Name, plan.Slug, plan.StripeProductID, plan.StripePriceID, plan.Description, plan.PriceUsd, plan.IsActive, plan.BillingCycle, plan.Features, plan.SortOrder, plan.MaxDescriptions, plan.MaxPhotos)),
		im.OnConflict("stripe_price_id").DoUpdate(
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
			im.SetCol("updated_at").To(psql.Raw("CURRENT_TIMESTAMP")),
		),
		im.Returning("*"),
	)
}

func FindPlanQuery(planID string) bob.Query {
	return psql.Select(
		sm.From("stripeflow_plans"),
		sm.Where(psql.Quote("stripe_price_id").EQ(psql.Arg(planID))),
	)
}

func GetPlansQuery() bob.Query {
	return psql.Select(
		sm.From("stripeflow_plans"),
		sm.Where(psql.Quote("is_active").EQ(psql.Arg(true))),
		sm.OrderBy("sort_order"),
	)
}

func UpsertSubscriptionQuery(sub *models.Subscription) bob.Query {
	return psql.Insert(
		im.Into("stripeflow_subscriptions", "stripe_customer_id", "stripe_subscription_id", "stripe_price_id", "user_id", "plan_name", "status", "usage_desc", "usage_photos", "date_start", "date_end", "date_renewal"),
		im.Values(psql.Arg(sub.StripeCustomerID, sub.StripeSubscriptionID, sub.StripePriceID, sub.UserID, sub.PlanName, sub.Status, sub.UsageDesc, sub.UsagePhotos, sub.DateStart, sub.DateEnd, sub.DateRenewal)),
		im.OnConflict("stripe_customer_id", "stripe_subscription_id").DoUpdate(
			im.SetExcluded("stripe_price_id"),
			im.SetExcluded("plan_name"),
			im.SetExcluded("status"),
			im.SetExcluded("usage_desc"),
			im.SetExcluded("usage_photos"),
			im.SetExcluded("date_start"),
			im.SetExcluded("date_end"),
			im.SetExcluded("date_renewal"),
			im.SetCol("updated_at").To(psql.Raw("CURRENT_TIMESTAMP")),
		),
		im.Returning("*"),
	)
}

func FindSubscriptionByUserIDQuery(userID string) bob.Query {
	return psql.Select(
		sm.From("stripeflow_subscriptions"),
		sm.Where(psql.Quote("user_id").EQ(psql.Arg(userID))),
		sm.OrderBy("date_start").Desc(),
		sm.Limit(1),
	)
}

func FindSubscriptionByStripeIDQuery(stripeSubID, stripeCustomerID string) bob.Query {
	return psql.Select(
		sm.From("stripeflow_subscriptions"),
		sm.Where(psql.Quote("stripe_subscription_id").EQ(psql.Arg(stripeSubID)).And(psql.Quote("stripe_customer_id").EQ(psql.Arg(stripeCustomerID)))),
	)
}

func DeleteSubscriptionQuery(id string) bob.Query {
	return psql.Delete(
		dm.From("stripeflow_subscriptions"),
		dm.Where(psql.Quote("id").EQ(psql.Arg(id))),
	)
}

func UpdateUsageQuery(userID string) bob.Query {
	return psql.Update(
		um.Table("stripeflow_subscriptions"),
		um.SetCol("usage_desc").To(psql.Raw("usage_desc - 1")),
		um.Where(psql.Quote("user_id").EQ(psql.Arg(userID))),
	)
}

func CheckActiveSubscriptionQuery(userID string) bob.Query {
	return psql.Select(
		sm.Columns(psql.F("COUNT", "id")),
		sm.From("stripeflow_subscriptions"),
		sm.Where(psql.Quote("user_id").EQ(psql.Arg(userID)).
			And(psql.Quote("status").EQ(psql.Arg("active"))).
			And(psql.Quote("date_renewal").GT(psql.Arg(time.Now().UTC())))),
	)
}

func CheckSubscriptionUsageQuery(userID string) bob.Query {
	return psql.Select(
		sm.Columns(psql.F("COUNT", "id")),
		sm.From("stripeflow_subscriptions"),
		sm.Where(psql.Quote("user_id").EQ(psql.Arg(userID)).
			And(psql.Quote("status").EQ(psql.Arg("active"))).
			And(psql.Quote("usage_desc").GT(psql.Arg(0))).
			And(psql.Quote("date_renewal").GT(psql.Arg(time.Now().UTC())))),
	)
}

// Repository methods

func (r *Repository) UpsertPlan(ctx context.Context, plan *models.Plan) (*models.Plan, error) {
	q := UpsertPlanQuery(plan)
	return bob.One(ctx, r.db, q, scan.StructMapper[*models.Plan]())
}

func (r *Repository) FindPlan(ctx context.Context, planID string) (*models.Plan, error) {
	q := FindPlanQuery(planID)
	return bob.One(ctx, r.db, q, scan.StructMapper[*models.Plan]())
}

func (r *Repository) GetPlans(ctx context.Context) ([]models.Plan, error) {
	q := GetPlansQuery()
	return bob.All(ctx, r.db, q, scan.StructMapper[models.Plan]())
}

func (r *Repository) UpsertSubscription(ctx context.Context, sub *models.Subscription) (*models.Subscription, error) {
	q := UpsertSubscriptionQuery(sub)
	return bob.One(ctx, r.db, q, scan.StructMapper[*models.Subscription]())
}

func (r *Repository) FindSubscriptionByUserID(ctx context.Context, userID string) (*models.Subscription, error) {
	q := FindSubscriptionByUserIDQuery(userID)
	return bob.One(ctx, r.db, q, scan.StructMapper[*models.Subscription]())
}

func (r *Repository) FindSubscriptionByStripeID(ctx context.Context, stripeSubID, stripeCustomerID string) (*models.Subscription, error) {
	q := FindSubscriptionByStripeIDQuery(stripeSubID, stripeCustomerID)
	return bob.One(ctx, r.db, q, scan.StructMapper[*models.Subscription]())
}

func (r *Repository) DeleteSubscription(ctx context.Context, id string) error {
	q := DeleteSubscriptionQuery(id)
	_, err := bob.Exec(ctx, r.db, q)
	return err
}

func (r *Repository) UpdateUsage(ctx context.Context, userID string) error {
	q := UpdateUsageQuery(userID)
	_, err := bob.Exec(ctx, r.db, q)
	return err
}

func (r *Repository) CheckActiveSubscription(ctx context.Context, userID string) (bool, error) {
	q := CheckActiveSubscriptionQuery(userID)
	count, err := bob.One(ctx, r.db, q, scan.SingleColumnMapper[int64])
	return count > 0, err
}

func (r *Repository) CheckSubscriptionUsage(ctx context.Context, userID string) (bool, error) {
	q := CheckSubscriptionUsageQuery(userID)
	count, err := bob.One(ctx, r.db, q, scan.SingleColumnMapper[int64])
	return count > 0, err
}
