package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AnalyticsDaily struct {
	Date        string `json:"date"`
	AccessCount int    `json:"access_count"`
}

type AnalyticsTopLink struct {
	ID          int    `json:"id"`
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	AccessCount int    `json:"access_count"`
}

type AnalyticsUserAgent struct {
	Browser string `json:"browser"`
	Count   int    `json:"count"`
}

type AnalyticsResponse struct {
	StartDate  string               `json:"start_date"`
	EndDate    string               `json:"end_date"`
	Slug       string               `json:"slug"`
	Daily      []AnalyticsDaily     `json:"daily"`
	TopLinks   []AnalyticsTopLink   `json:"top_links"`
	UserAgents []AnalyticsUserAgent `json:"user_agents"`
}

func (h *Handler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	start, end, err := parseAnalyticsDateRange(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if slug != "" && !slugPattern.MatchString(slug) {
		http.Error(w, "invalid slug filter", http.StatusBadRequest)
		return
	}

	topN := parsePositiveInt(r.URL.Query().Get("top"), 10)
	if topN > 50 {
		topN = 50
	}

	daily, err := h.queryDailyAnalytics(r, start, end, slug)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	topLinks, err := h.queryTopLinks(r, start, end, slug, topN)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	userAgents, err := h.queryUserAgents(r, start, end, slug)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AnalyticsResponse{
		StartDate:  start.Format("2006-01-02"),
		EndDate:    end.Format("2006-01-02"),
		Slug:       slug,
		Daily:      daily,
		TopLinks:   topLinks,
		UserAgents: userAgents,
	})
}

func parseUserAgent(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "Edge/"):
		return "Edge"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera"):
		return "Opera"
	case strings.Contains(ua, "Chrome/"):
		return "Chrome"
	case strings.Contains(ua, "Firefox/"):
		return "Firefox"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		return "Safari"
	case strings.Contains(ua, "curl/"):
		return "curl"
	case strings.Contains(strings.ToLower(ua), "python"):
		return "Python"
	case strings.Contains(ua, "Go-http-client"):
		return "Go HTTP"
	case ua == "":
		return "Unknown"
	default:
		return "Other"
	}
}

func (h *Handler) queryUserAgents(r *http.Request, start, end time.Time, slug string) ([]AnalyticsUserAgent, error) {
	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02")}
	where := "WHERE date(e.accessed_at) BETWEEN date(?) AND date(?)"
	if slug != "" {
		where += " AND l.slug = ?"
		args = append(args, slug)
	}

	query := `
		SELECT e.user_agent
		FROM link_access_events e
		JOIN links l ON l.id = e.link_id
		` + where

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var ua string
		if err := rows.Scan(&ua); err != nil {
			continue
		}
		counts[parseUserAgent(ua)]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]AnalyticsUserAgent, 0, len(counts))
	for browser, count := range counts {
		items = append(items, AnalyticsUserAgent{Browser: browser, Count: count})
	}
	// sort descending by count
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Count > items[j-1].Count; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	return items, nil
}

func parseAnalyticsDateRange(r *http.Request) (time.Time, time.Time, error) {
	startRaw := strings.TrimSpace(r.URL.Query().Get("start_date"))
	endRaw := strings.TrimSpace(r.URL.Query().Get("end_date"))
	today := time.Now().UTC()

	var start time.Time
	var end time.Time
	var err error

	switch {
	case startRaw == "" && endRaw == "":
		end = today
		start = end.AddDate(0, 0, -29)
	case startRaw != "" && endRaw != "":
		start, err = time.Parse("2006-01-02", startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date")
		}
		end, err = time.Parse("2006-01-02", endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date")
		}
	case startRaw != "":
		start, err = time.Parse("2006-01-02", startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date")
		}
		end = today
	case endRaw != "":
		end, err = time.Parse("2006-01-02", endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date")
		}
		start = end.AddDate(0, 0, -29)
	}

	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start_date cannot be after end_date")
	}
	return start.UTC(), end.UTC(), nil
}

func (h *Handler) queryDailyAnalytics(r *http.Request, start, end time.Time, slug string) ([]AnalyticsDaily, error) {
	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02"), start.Format("2006-01-02"), end.Format("2006-01-02")}
	filter := ""
	if slug != "" {
		filter = " AND l.slug = ?"
		args = append(args, slug)
	}

	query := `
		WITH RECURSIVE dates(d) AS (
			SELECT date(?)
			UNION ALL
			SELECT date(d, '+1 day') FROM dates WHERE d < date(?)
		), counts AS (
			SELECT date(e.accessed_at) AS day, COUNT(*) AS c
			FROM link_access_events e
			JOIN links l ON l.id = e.link_id
			WHERE date(e.accessed_at) BETWEEN date(?) AND date(?)` + filter + `
			GROUP BY date(e.accessed_at)
		)
		SELECT dates.d, COALESCE(counts.c, 0)
		FROM dates
		LEFT JOIN counts ON counts.day = dates.d
		ORDER BY dates.d ASC`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]AnalyticsDaily, 0)
	for rows.Next() {
		var p AnalyticsDaily
		if err := rows.Scan(&p.Date, &p.AccessCount); err != nil {
			continue
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return points, nil
}

func (h *Handler) queryTopLinks(r *http.Request, start, end time.Time, slug string, topN int) ([]AnalyticsTopLink, error) {
	args := []any{start.Format("2006-01-02"), end.Format("2006-01-02")}
	where := "WHERE date(e.accessed_at) BETWEEN date(?) AND date(?)"
	if slug != "" {
		where += " AND l.slug = ?"
		args = append(args, slug)
	}
	args = append(args, topN)

	query := `
		SELECT l.id, l.slug, l.url, COUNT(*) AS c
		FROM link_access_events e
		JOIN links l ON l.id = e.link_id
		` + where + `
		GROUP BY l.id, l.slug, l.url
		ORDER BY c DESC, l.slug ASC
		LIMIT ?`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]AnalyticsTopLink, 0)
	for rows.Next() {
		var item AnalyticsTopLink
		if err := rows.Scan(&item.ID, &item.Slug, &item.URL, &item.AccessCount); err != nil {
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
