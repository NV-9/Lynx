package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type UserRecord struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

func (h *Handler) AdminPage(w http.ResponseWriter, r *http.Request) {
	h.serveHTML(w, "admin.html")
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, username, is_admin, created_at FROM users ORDER BY created_at DESC`,
	)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := make([]UserRecord, 0)
	for rows.Next() {
		var u UserRecord
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
			continue
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": users})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	username, password, isAdmin, err := parseCreateUserRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if !usernamePattern.MatchString(username) {
		http.Error(w, "username must be 3-32 chars and only contain letters, numbers, _, ., -", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)`,
		username, string(hash), isAdmin,
	)
	if err != nil {
		http.Error(w, "username already exists", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) SetUserAdmin(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || userID < 1 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var body struct {
		IsAdmin bool `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	var wasAdmin bool
	if err := h.db.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&wasAdmin); err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	if wasAdmin && !body.IsAdmin {
		var adminCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&adminCount); err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		if adminCount <= 1 {
			http.Error(w, "cannot remove admin from the last admin account", http.StatusBadRequest)
			return
		}
	}

	res, err := h.db.ExecContext(r.Context(),
		`UPDATE users SET is_admin = ? WHERE id = ?`, body.IsAdmin, userID,
	)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	current, err := h.currentUser(r)
	if err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || userID < 1 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if current.ID == userID {
		http.Error(w, "admins cannot delete their own account", http.StatusBadRequest)
		return
	}

	var isAdmin bool
	if err := h.db.QueryRowContext(r.Context(), `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&isAdmin); err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if isAdmin {
		var adminCount int
		if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&adminCount); err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		if adminCount <= 1 {
			http.Error(w, "cannot delete the last admin account", http.StatusBadRequest)
			return
		}
	}

	res, err := h.db.ExecContext(r.Context(), `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
}

func parseCreateUserRequest(r *http.Request) (string, string, bool, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			IsAdmin  bool   `json:"is_admin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return "", "", false, errors.New("invalid JSON")
		}
		if strings.TrimSpace(body.Username) == "" || body.Password == "" {
			return "", "", false, errors.New("username and password are required")
		}
		return strings.TrimSpace(body.Username), body.Password, body.IsAdmin, nil
	}

	if strings.Contains(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return "", "", false, errors.New("invalid form")
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return "", "", false, errors.New("invalid form")
		}
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		return "", "", false, errors.New("username and password are required")
	}
	isAdmin := r.FormValue("is_admin") == "true" || r.FormValue("is_admin") == "on" || r.FormValue("is_admin") == "1"
	return username, password, isAdmin, nil
}
