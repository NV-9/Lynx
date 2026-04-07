package middleware

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const UserIDKey contextKey = "userID"

func RequireAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAPI := strings.HasPrefix(r.URL.Path, "/api/")
			deny := func() {
				if isAPI {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
				} else {
					http.Redirect(w, r, "/login", http.StatusSeeOther)
				}
			}

			cookie, err := r.Cookie("session_id")
			if err != nil {
				deny()
				return
			}

			var userID int
			var expiresAt time.Time
			err = db.QueryRowContext(r.Context(),
				`SELECT user_id, expires_at FROM sessions WHERE id = ?`, cookie.Value,
			).Scan(&userID, &expiresAt)
			if err != nil {
				log.Printf("[auth] session lookup failed (id=%.8s...): %v", cookie.Value, err)
				deny()
				return
			}

			if time.Now().UTC().After(expiresAt.UTC()) {
				log.Printf("[auth] session %.8s... expired (expires_at=%s)", cookie.Value, expiresAt.UTC().Format(time.RFC3339))
				deny()
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAdmin(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v := r.Context().Value(UserIDKey)
			if v == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			userID, ok := v.(int)
			if !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			var isAdmin bool
			err := db.QueryRowContext(r.Context(),
				`SELECT is_admin FROM users WHERE id = ?`, userID,
			).Scan(&isAdmin)
			if err != nil || !isAdmin {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
