package handler

import (
	"context"
	"net/http"
	"stripeflow/repository"

	"github.com/josuebrunel/gopkg/xlog"
)

type UserResolver interface {
	GetUser(ctx context.Context) (string, error)
}

func SubscriptionActiveMiddleware(repo repository.Querier, userResolver UserResolver, fallbackHandler http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := userResolver.GetUser(r.Context())
			if err != nil || userID == "" {
				xlog.Error("SubscriptionActiveMiddleware: could not get user", "error", err)
				fallbackHandler(w, r)
				return
			}

			active, err := repo.CheckActiveSubscription(r.Context(), userID)
			if err != nil || !active {
				xlog.Debug("SubscriptionActiveMiddleware: no active subscription", "user_id", userID, "error", err)
				fallbackHandler(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
