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

func SubscriptionActiveMiddleware(repo repository.Querier, userResolver UserResolver, devMode bool, fallbackHandler http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if devMode {
				xlog.Debug("SubscriptionActiveMiddleware: isDev=true")
				next.ServeHTTP(w, r)
				return
			}

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

func SubscriptionUsageMiddleware(repo repository.Querier, userResolver UserResolver, devMode bool, fallbackHandler http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if devMode {
				xlog.Debug("SubscriptionUsageMiddleware: isDev=true")
				next.ServeHTTP(w, r)
				return
			}

			userID, err := userResolver.GetUser(r.Context())
			if err != nil || userID == "" {
				xlog.Error("SubscriptionUsageMiddleware: could not get user", "error", err)
				fallbackHandler(w, r)
				return
			}

			hasUsage, err := repo.CheckSubscriptionUsage(r.Context(), userID)
			if err != nil || !hasUsage {
				xlog.Debug("SubscriptionUsageMiddleware: usage limit reached or no active subscription", "user_id", userID, "error", err)
				fallbackHandler(w, r)
				return
			}

			// Call next handler
			next.ServeHTTP(w, r)

			// Update usage after successful request processing
			if err := repo.UpdateUsage(r.Context(), userID); err != nil {
				xlog.Error("SubscriptionUsageMiddleware: error updating usage", "user_id", userID, "error", err)
			}
		})
	}
}
