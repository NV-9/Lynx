package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type Link struct {
	ID        int       `json:"id"`
	Slug      string    `json:"slug"`
	URL       string    `json:"url"`
	Accesses  int       `json:"access_count"`
	CreatedAt time.Time `json:"created_at"`
}

type LinksResponse struct {
	Items      []Link `json:"items"`
	Page       int    `json:"page"`
	Size       int    `json:"size"`
	Total      int    `json:"total"`
	TotalPages int    `json:"total_pages"`
	Filter     string `json:"filter"`
}

var slugPattern = regexp.MustCompile(`^[A-Za-z0-9]{3,32}$`)
var reservedSlugs = map[string]struct{}{
	"admin":   {},
	"api":     {},
	"login":   {},
	"logout":  {},
	"setup":   {},
	"static":  {},
	"favicon": {},
}

func (h *Handler) GetLinks(w http.ResponseWriter, r *http.Request) {
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	size := parsePositiveInt(r.URL.Query().Get("size"), 20)
	if size > 100 {
		size = 100
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))

	where := ""
	args := []any{}
	if filter != "" {
		where = ` WHERE slug LIKE ? OR url LIKE ?`
		needle := "%" + filter + "%"
		args = append(args, needle, needle)
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM links` + where
	if err := h.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&total); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	offset := (page - 1) * size
	query := `SELECT id, slug, url, clicks, created_at FROM links` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, size, offset)

	rows, err := h.db.QueryContext(r.Context(),
		query,
		args...,
	)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	links := []Link{}
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.Slug, &l.URL, &l.Accesses, &l.CreatedAt); err != nil {
			continue
		}
		links = append(links, l)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + size - 1) / size
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LinksResponse{
		Items:      links,
		Page:       page,
		Size:       size,
		Total:      total,
		TotalPages: totalPages,
		Filter:     filter,
	})
}

func (h *Handler) ShortenURL(w http.ResponseWriter, r *http.Request) {
	longURL, customSlug, err := parseShortenRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	slug := strings.TrimSpace(customSlug)
	if slug != "" {
		if !slugPattern.MatchString(slug) {
			http.Error(w, "invalid slug: use 3-32 alphanumeric characters", http.StatusBadRequest)
			return
		}
		if isReservedSlug(slug) {
			http.Error(w, "slug is reserved", http.StatusBadRequest)
			return
		}
		if _, err := h.db.ExecContext(r.Context(),
			`INSERT INTO links (slug, url) VALUES (?, ?)`, slug, longURL,
		); err != nil {
			http.Error(w, "slug already exists", http.StatusConflict)
			return
		}
	} else {
		var insertErr error
		for range 8 {
			slug = generateSlug()
			if isReservedSlug(slug) {
				continue
			}
			_, insertErr = h.db.ExecContext(r.Context(),
				`INSERT INTO links (slug, url) VALUES (?, ?)`, slug, longURL,
			)
			if insertErr == nil {
				break
			}
		}
		if insertErr != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
	}

	shortURL := h.baseURL + "/" + slug

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="mt-4 p-4 bg-green-50 border border-green-200 rounded-lg">
  <p class="text-xs text-gray-500 mb-1">Your short link:</p>
  <a href="%s" target="_blank" class="text-blue-600 hover:text-blue-800 underline font-mono text-sm break-all">%s</a>
</div>`, shortURL, shortURL)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"short_url": shortURL, "slug": slug})
}

func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id < 1 {
		http.Error(w, "invalid link id", http.StatusBadRequest)
		return
	}

	res, err := h.db.ExecContext(r.Context(), `DELETE FROM links WHERE id = ?`, id)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		http.Error(w, "link not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseShortenRequest(r *http.Request) (string, string, error) {
	validateLongURL := func(raw string) (string, error) {
		u := strings.TrimSpace(raw)
		if u == "" {
			return "", fmt.Errorf("url is required")
		}
		if len(u) > 2048 {
			return "", fmt.Errorf("url is too long")
		}
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "", fmt.Errorf("invalid url")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("url must start with http:// or https://")
		}
		return u, nil
	}

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			URL  string `json:"url"`
			Slug string `json:"slug"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return "", "", fmt.Errorf("invalid JSON")
		}
		u, err := validateLongURL(body.URL)
		if err != nil {
			return "", "", err
		}
		return u, body.Slug, nil
	}

	if err := r.ParseForm(); err != nil {
		return "", "", fmt.Errorf("invalid form")
	}
	u, err := validateLongURL(r.FormValue("url"))
	if err != nil {
		return "", "", err
	}
	return u, r.FormValue("slug"), nil
}

func parsePositiveInt(raw string, fallback int) int {
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return fallback
	}
	return v
}

func isReservedSlug(slug string) bool {
	normalized := strings.ToLower(strings.TrimSpace(slug))
	if normalized == "" {
		return true
	}
	if _, ok := reservedSlugs[normalized]; ok {
		return true
	}
	return strings.HasPrefix(normalized, "api/") || strings.HasPrefix(normalized, "static/")
}
