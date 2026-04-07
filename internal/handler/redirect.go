package handler

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) RedirectSlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	var linkID int
	var longURL string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, url FROM links WHERE slug = ?`, slug,
	).Scan(&linkID, &longURL)
	if err == sql.ErrNoRows {
		h.NotFoundPage(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	userAgent := r.Header.Get("User-Agent")
	h.db.ExecContext(r.Context(), `UPDATE links SET clicks = clicks + 1 WHERE slug = ?`, slug)
	h.db.ExecContext(r.Context(), `INSERT INTO link_access_events (link_id, user_agent) VALUES (?, ?)`, linkID, userAgent)
	http.Redirect(w, r, longURL, http.StatusFound)
}
