package middleware

import (
	"net/http"
	"strings"

	"github.com/hostoria/hostoria-node/internal/config"
)

// NodeAuth validates the Authorization: Bearer {token} header against the node's token.
// Returns 401 if missing or 403 if wrong.
func NodeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := config.Get()
		if cfg == nil {
			http.Error(w, `{"error":"configuration not loaded"}`, http.StatusInternalServerError)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"missing Authorization header"}`))
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// No "Bearer " prefix
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid Authorization scheme"}`))
			return
		}

		if token != cfg.Token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"invalid token"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}
