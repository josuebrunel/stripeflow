package repository

import (
	"context"
	"stripeflow/db/models"
)

type Querier interface {
	UpsertPlan(ctx context.Context, plan *models.Plan) (*models.Plan, error)
	FindPlan(ctx context.Context, planID string) (*models.Plan, error)
	GetPlans(ctx context.Context) ([]models.Plan, error)
	UpsertSubscription(ctx context.Context, sub *models.Subscription) (*models.Subscription, error)
	FindSubscriptionByUserID(ctx context.Context, userID string) (*models.Subscription, error)
	FindSubscriptionByStripeID(ctx context.Context, stripeSubID, stripeCustomerID string) (*models.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error
	UpdateUsage(ctx context.Context, userID string) error
	CheckActiveSubscription(ctx context.Context, userID string) (bool, error)
	CheckSubscriptionUsage(ctx context.Context, userID string) (bool, error)
}
