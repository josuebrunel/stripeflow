package subscription

import (
	"net/http"
	"propcopyai/pkg/auth"
	"propcopyai/pkg/config"
	"propcopyai/pkg/db/models"
	"propcopyai/pkg/toast"
	"time"

	"github.com/josuebrunel/gopkg/etr"

	"github.com/josuebrunel/gopkg/xlog"
	"github.com/labstack/echo/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
)

func SubscriptionActiveMiddleware(db bob.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			var (
				user = auth.GetUser(c.Request().Context())
				ctx  = c.Request().Context()
			)

			if config.IsAppEnvDev() {
				xlog.Debug("SubscriptionActiveMiddleware", "user", user.Email, "isDev", true)
				return next(c)
			}

			exists, err := models.Subscriptions.Query(
				sm.Where(
					psql.Quote("user_id").EQ(psql.Arg(user.ID)).
						And(psql.Quote("status").EQ(psql.Arg(StatusActive))).
						And(psql.Quote("date_renewal").GT(psql.Arg(time.Now().UTC()))),
				),
			).Exists(ctx, db)
			xlog.Debug("SubscriptionActiveMiddleware", "user", user.Email, "exists", exists, "err", err)
			if err != nil || !exists {
				plans, _ := getPlans(ctx, db)
				toast.NotifyInfo(c, "No active subscription found.")
				return etr.Render(c, http.StatusOK, PlansView(plans), nil)
			}

			return next(c)
		}
	}
}

func SubscriptionUsageMiddleware(db bob.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			var (
				user = auth.GetUser(c.Request().Context())
				ctx  = c.Request().Context()
			)

			if config.IsAppEnvDev() {
				xlog.Debug("SubscriptionUsageMiddleware", "user", user.Email, "isDev", true)
				return next(c)
			}

			exists, err := models.Subscriptions.Query(
				sm.Where(
					psql.Quote("user_id").EQ(psql.Arg(user.ID)).
						And(psql.Quote("status").EQ(psql.Arg(StatusActive))).
						And(psql.Quote("usage_desc").GT(psql.Arg(0))).
						And(psql.Quote("date_renewal").GT(psql.Arg(time.Now().UTC()))),
				),
			).Exists(ctx, db)
			xlog.Debug("SubcriptionUsageMiddleware", "user", user.Email, "exists", exists, "err", err)
			if !exists || err != nil {
				xlog.Error("Error while trying to get user's subscription", "user", user.Email, "error", err)
				plans, _ := getPlans(ctx, db)
				toast.NotifyInfo(c, "No active subscription found or usage limit reached.")
				return etr.Render(c, http.StatusOK, PlansView(plans), nil)
			}
			resp := next(c)
			if resp != nil {
				return resp
			}
			if err := UsedBy(ctx, db, user.ID); err != nil {
				xlog.Error("Error while trying to update user's subscription", "user", user.Email, "error", err)
			}
			return resp
		}
	}
}
