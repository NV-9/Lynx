package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/nv-9/lynx/internal/middleware"
)

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func userIDFromRequest(r *http.Request) (int, bool) {
	v := r.Context().Value(middleware.UserIDKey)
	if v == nil {
		return 0, false
	}

	switch id := v.(type) {
	case int:
		return id, true
	case int64:
		return int(id), true
	case string:
		parsed, err := strconv.Atoi(id)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func (h *Handler) currentUser(r *http.Request) (User, error) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		return User{}, sql.ErrNoRows
	}

	var u User
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, username, is_admin FROM users WHERE id = ?`, userID,
	).Scan(&u.ID, &u.Username, &u.IsAdmin)
	return u, err
}

func (h *Handler) CurrentUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.currentUser(r)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(u)
}
