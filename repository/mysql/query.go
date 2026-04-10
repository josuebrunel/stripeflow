package mysql

import (
	"context"
	"stripeflow/db/models"
	"time"

	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/mysql"
	"github.com/stephenafamo/bob/dialect/mysql/im"
	"github.com/stephenafamo/bob/dialect/mysql/sm"
	"github.com/stephenafamo/bob/dialect/mysql/dm"
	"github.com/stephenafamo/scan"
	"github.com/google/uuid"
)

func UpsertPlanQuery(plan *models.Plan) bob.Query {
	if plan.ID == "" {
		plan.ID = uuid.NewString()
	}
	return mysql.Insert(
		im.Into("stripeflow_plans", "id", "name", "slug", "stripe_product_id", "stripe_price_id", "description", "price_usd", "is_active", "billing_cycle", "features", "sort_order", "metadata"),
		im.Values(mysql.Arg(plan.ID, plan.Name, plan.Slug, plan.StripeProductID, plan.StripePriceID, plan.Description, plan.PriceUsd, plan.IsActive, plan.BillingCycle, plan.Features, plan.SortOrder, plan.Metadata)),
		im.OnDuplicateKeyUpdate(
			im.UpdateCol("name").To(mysql.Raw("VALUES(name)")),
			im.UpdateCol("slug").To(mysql.Raw("VALUES(slug)")),
			im.UpdateCol("stripe_product_id").To(mysql.Raw("VALUES(stripe_product_id)")),
			im.UpdateCol("description").To(mysql.Raw("VALUES(description)")),
			im.UpdateCol("price_usd").To(mysql.Raw("VALUES(price_usd)")),
			im.UpdateCol("is_active").To(mysql.Raw("VALUES(is_active)")),
			im.UpdateCol("billing_cycle").To(mysql.Raw("VALUES(billing_cycle)")),
			im.UpdateCol("features").To(mysql.Raw("VALUES(features)")),
			im.UpdateCol("sort_order").To(mysql.Raw("VALUES(sort_order)")),
			im.UpdateCol("metadata").To(mysql.Raw("VALUES(metadata)")),
			im.UpdateCol("updated_at").To(mysql.Raw("CURRENT_TIMESTAMP")),
		),
	)
}

func FindPlanQuery(planID string) bob.Query {
	return mysql.Select(
		sm.From("stripeflow_plans"),
		sm.Where(mysql.Quote("stripe_price_id").EQ(mysql.Arg(planID))),
	)
}

func GetPlansQuery() bob.Query {
	return mysql.Select(
		sm.From("stripeflow_plans"),
		sm.Where(mysql.Quote("is_active").EQ(mysql.Arg(true))),
		sm.OrderBy("sort_order"),
	)
}

func UpsertSubscriptionQuery(sub *models.Subscription) bob.Query {
	if sub.ID == "" {
		sub.ID = uuid.NewString()
	}
	return mysql.Insert(
		im.Into("stripeflow_subscriptions", "id", "stripe_customer_id", "stripe_subscription_id", "stripe_price_id", "user_id", "plan_name", "status", "metadata", "date_start", "date_end", "date_renewal"),
		im.Values(mysql.Arg(sub.ID, sub.StripeCustomerID, sub.StripeSubscriptionID, sub.StripePriceID, sub.UserID, sub.PlanName, sub.Status, sub.Metadata, sub.DateStart, sub.DateEnd, sub.DateRenewal)),
		im.OnDuplicateKeyUpdate(
			im.UpdateCol("stripe_price_id").To(mysql.Raw("VALUES(stripe_price_id)")),
			im.UpdateCol("plan_name").To(mysql.Raw("VALUES(plan_name)")),
			im.UpdateCol("status").To(mysql.Raw("VALUES(status)")),
			im.UpdateCol("metadata").To(mysql.Raw("VALUES(metadata)")),
			im.UpdateCol("date_start").To(mysql.Raw("VALUES(date_start)")),
			im.UpdateCol("date_end").To(mysql.Raw("VALUES(date_end)")),
			im.UpdateCol("date_renewal").To(mysql.Raw("VALUES(date_renewal)")),
			im.UpdateCol("updated_at").To(mysql.Raw("CURRENT_TIMESTAMP")),
		),
	)
}

func FindSubscriptionByUserIDQuery(userID string) bob.Query {
	return mysql.Select(
		sm.From("stripeflow_subscriptions"),
		sm.Where(mysql.Quote("user_id").EQ(mysql.Arg(userID))),
		sm.OrderBy("date_start").Desc(),
		sm.Limit(1),
	)
}

func FindSubscriptionByStripeIDQuery(stripeSubID, stripeCustomerID string) bob.Query {
	return mysql.Select(
		sm.From("stripeflow_subscriptions"),
		sm.Where(mysql.Quote("stripe_subscription_id").EQ(mysql.Arg(stripeSubID)).And(mysql.Quote("stripe_customer_id").EQ(mysql.Arg(stripeCustomerID)))),
	)
}

func DeleteSubscriptionQuery(id string) bob.Query {
	return mysql.Delete(
		dm.From("stripeflow_subscriptions"),
		dm.Where(mysql.Quote("id").EQ(mysql.Arg(id))),
	)
}

func CheckActiveSubscriptionQuery(userID string) bob.Query {
	return mysql.Select(
		sm.Columns(mysql.F("COUNT", "id")),
		sm.From("stripeflow_subscriptions"),
		sm.Where(mysql.Quote("user_id").EQ(mysql.Arg(userID)).
			And(mysql.Quote("status").In(mysql.Arg("active", "trialing"))).
			And(mysql.Quote("date_renewal").GT(mysql.Arg(time.Now().UTC())))),
	)
}

// Repository methods

func (r *Repository) UpsertPlan(ctx context.Context, plan *models.Plan) (*models.Plan, error) {
	q := UpsertPlanQuery(plan)
	_, err := bob.Exec(ctx, r.db, q)
	if err != nil {
		return nil, err
	}
	return r.FindPlan(ctx, plan.StripePriceID)
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
	_, err := bob.Exec(ctx, r.db, q)
	if err != nil {
		return nil, err
	}
	return r.FindSubscriptionByStripeID(ctx, sub.StripeSubscriptionID, sub.StripeCustomerID)
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

func (r *Repository) CheckActiveSubscription(ctx context.Context, userID string) (bool, error) {
	q := CheckActiveSubscriptionQuery(userID)
	count, err := bob.One(ctx, r.db, q, scan.SingleColumnMapper[int64])
	return count > 0, err
}
