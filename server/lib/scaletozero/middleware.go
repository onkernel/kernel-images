package scaletozero

import (
	"context"
	"net/http"

	"github.com/onkernel/kernel-images/server/lib/logger"
)

// Middleware returns a standard net/http middleware that disables scale-to-zero
// at the start of each request and re-enables it after the handler completes.
func Middleware(ctrl Controller) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := ctrl.Disable(r.Context()); err != nil {
				logger.FromContext(r.Context()).Error("failed to disable scale-to-zero", "error", err)
				http.Error(w, "failed to disable scale-to-zero", http.StatusInternalServerError)
				return
			}
			defer ctrl.Enable(context.WithoutCancel(r.Context()))

			next.ServeHTTP(w, r)
		})
	}
}
