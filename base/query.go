package subscription

import (
	"context"
	"propcopyai/pkg/db/models"

	"github.com/gofrs/uuid/v5"
	"github.com/josuebrunel/gopkg/xlog"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	"github.com/stephenafamo/scan"
)

func UsedBy(ctx context.Context, db bob.DB, userID string) error {
	query := psql.Update(
		um.Table("subscriptions"),
		um.SetCol("usage_desc").To("usage_desc - 1"),
		um.Where(psql.Quote("user_id").EQ(psql.Arg(userID))),
	)
	if _, err := bob.Exec(ctx, db, query); err != nil {
		xlog.Error("Failed to update usage", "user", userID, "error", err)
		return err
	}
	return nil
}

func getPlans(ctx context.Context, db bob.DB) (models.PlanSlice, error) {
	plans, err := models.Plans.Query(
		sm.Where(psql.Quote("is_active").EQ(psql.Arg(true))),
		sm.OrderBy("sort_order"),
	).All(ctx, db)
	return plans, err
}

type DBUser struct {
	ID    uuid.UUID
	Email string
}

func getUserByEmail(ctx context.Context, db bob.DB, email string) (uuid.UUID, error) {
	user, err := bob.One(ctx, db,
		psql.Select(
			sm.Columns("id", "email"),
			sm.From("ezauth_users"),
			sm.Where(psql.Quote("email").EQ(psql.Arg(email))),
		),
		scan.StructMapper[DBUser]())
	if err != nil {
		xlog.Error("Failed to get user for email", "email", email, "error", err)
		return uuid.UUID{}, err
	}
	return user.ID, nil
}

func GetUserSubscription(ctx context.Context, db bob.DB, userID string) (*models.Subscription, error) {
	sub, err := models.Subscriptions.Query(
		sm.Where(psql.Quote("user_id").EQ(psql.Arg(userID))),
	).One(ctx, db)
	if err != nil {
		xlog.Error("Error while getting subscription for user", "user", userID, "error", err)
		return nil, err
	}
	return sub, nil
}

func GetUserRemainingUsage(ctx context.Context, db bob.DB, userID string) (int64, error) {
	sub, err := GetUserSubscription(ctx, db, userID)
	if err != nil {
		xlog.Error("Failed to get subscription for user", "user", userID, "error", err)
		return 0, nil
	}
	return int64(sub.UsageDesc.GetOrZero()), nil
}
