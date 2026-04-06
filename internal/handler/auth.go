package handler

import (
	"database/sql"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var setupUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	h.serveHTML(w, "index.html")
}

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.needsSetup(r) {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	if h.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.serveHTML(w, "login.html")
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	var userID int
	var hash string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE username = ?`, username,
	).Scan(&userID, &hash)

	if err == sql.ErrNoRows || (err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil) {
		http.Redirect(w, r, "/login?error="+url.QueryEscape("Invalid username or password"), http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if err := h.createSession(w, r, userID); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) SetupPage(w http.ResponseWriter, r *http.Request) {
	if !h.needsSetup(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.serveHTML(w, "setup.html")
}

func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	if !h.needsSetup(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Redirect(w, r, "/setup?error="+url.QueryEscape("Username and password are required"), http.StatusSeeOther)
		return
	}
	if !setupUsernamePattern.MatchString(username) {
		http.Redirect(w, r, "/setup?error="+url.QueryEscape("Username must be 3-32 chars and only contain letters, numbers, _, ., -"), http.StatusSeeOther)
		return
	}
	if len(password) < 8 {
		http.Redirect(w, r, "/setup?error="+url.QueryEscape("Password must be at least 8 characters"), http.StatusSeeOther)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	result, err := h.db.ExecContext(r.Context(),
		`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, 1)`, username, string(hash),
	)
	if err != nil {
		http.Redirect(w, r, "/setup?error="+url.QueryEscape("Username already taken"), http.StatusSeeOther)
		return
	}

	userID, _ := result.LastInsertId()
	if err := h.createSession(w, r, int(userID)); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil {
		h.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) needsSetup(r *http.Request) bool {
	var count int
	h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count)
	return count == 0
}

func (h *Handler) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return false
	}
	var expiresAt time.Time
	err = h.db.QueryRowContext(r.Context(),
		`SELECT expires_at FROM sessions WHERE id = ?`, cookie.Value,
	).Scan(&expiresAt)
	if err != nil {
		return false
	}
	return time.Now().UTC().Before(expiresAt.UTC())
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request, userID int) error {
	sessionID, err := generateSessionID()
	if err != nil {
		return err
	}
	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)
	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		sessionID, userID, expiresAt,
	)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
