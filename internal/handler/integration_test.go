package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/nv-9/lynx/internal/database"
	"github.com/nv-9/lynx/internal/handler"
	"github.com/nv-9/lynx/internal/middleware"
	"golang.org/x/crypto/bcrypt"
)

func testRouter(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "links.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	staticFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>index</html>")},
		"login.html": &fstest.MapFile{Data: []byte("<html>login</html>")},
		"setup.html": &fstest.MapFile{Data: []byte("<html>setup</html>")},
		"admin.html": &fstest.MapFile{Data: []byte("<html>admin</html>")},
		"404.html":   &fstest.MapFile{Data: []byte("<html>custom 404</html>")},
	}

	h := handler.New(db, "http://localhost:8080", staticFS)

	r := chi.NewRouter()
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.EnforceSameOriginWrite("http://localhost:8080"))
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.Login)
	r.Get("/setup", h.SetupPage)
	r.Post("/setup", h.Setup)
	r.Post("/logout", h.Logout)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(db))
		r.Get("/", h.Dashboard)
		r.Get("/api/me", h.CurrentUser)
		r.Get("/api/links", h.GetLinks)
		r.Get("/api/analytics", h.GetAnalytics)
		r.Post("/api/shorten", h.ShortenURL)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAdmin(db))
			r.Get("/admin", h.AdminPage)
			r.Get("/api/users", h.ListUsers)
			r.Post("/api/users", h.CreateUser)
			r.Patch("/api/users/{id}/admin", h.SetUserAdmin)
			r.Delete("/api/users/{id}", h.DeleteUser)
			r.Delete("/api/links/{id}", h.DeleteLink)
		})
	})

	r.Get("/{slug}", h.RedirectSlug)
	r.NotFound(h.NotFoundPage)

	return db, r
}

func mustCreateUser(t *testing.T, db *sql.DB, username, password string, isAdmin bool) int {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	res, err := db.Exec(`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)`, username, string(hash), isAdmin)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return int(id)
}

func mustCreateSession(t *testing.T, db *sql.DB, userID int) string {
	t.Helper()

	sid := fmt.Sprintf("sid-%d-%d", userID, time.Now().UnixNano())
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	if _, err := db.Exec(`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`, sid, userID, expiresAt); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return sid
}

func doAuthedRequest(t *testing.T, router http.Handler, method, path, body, contentType, sessionID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Origin", "http://localhost:8080")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestSetupCreatesAdminUser(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	form := url.Values{}
	form.Set("username", "owner")
	form.Set("password", "password123")

	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}

	var isAdmin bool
	err := db.QueryRow(`SELECT is_admin FROM users WHERE username = ?`, "owner").Scan(&isAdmin)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if !isAdmin {
		t.Fatal("first setup user should be admin")
	}
}

func TestCustomSlugCreation(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	userID := mustCreateUser(t, db, "alice", "password123", true)
	sid := mustCreateSession(t, db, userID)

	body := "url=https%3A%2F%2Fexample.com%2F1&slug=mycustom1"
	rr := doAuthedRequest(t, router, http.MethodPost, "/api/shorten", body, "application/x-www-form-urlencoded", sid)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["slug"] != "mycustom1" {
		t.Fatalf("expected custom slug to be used, got %q", payload["slug"])
	}

	rrDup := doAuthedRequest(t, router, http.MethodPost, "/api/shorten", body, "application/x-www-form-urlencoded", sid)
	if rrDup.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate custom slug, got %d", rrDup.Code)
	}
}

func TestReservedSlugRejected(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	userID := mustCreateUser(t, db, "alice", "password123", true)
	sid := mustCreateSession(t, db, userID)

	body := "url=https%3A%2F%2Fexample.com%2F1&slug=admin"
	rr := doAuthedRequest(t, router, http.MethodPost, "/api/shorten", body, "application/x-www-form-urlencoded", sid)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for reserved slug, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLinksFilterAndPagination(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	userID := mustCreateUser(t, db, "alice", "password123", true)
	sid := mustCreateSession(t, db, userID)

	for i := 1; i <= 30; i++ {
		slug := fmt.Sprintf("item%02d", i)
		target := fmt.Sprintf("https://example.com/%d", i)
		if i%3 == 0 {
			slug = fmt.Sprintf("alpha%02d", i)
		}
		if _, err := db.Exec(`INSERT INTO links (slug, url) VALUES (?, ?)`, slug, target); err != nil {
			t.Fatalf("insert link %d: %v", i, err)
		}
	}

	rrPage2 := doAuthedRequest(t, router, http.MethodGet, "/api/links?page=2&size=20", "", "", sid)
	if rrPage2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rrPage2.Code)
	}

	var page2 struct {
		Items      []handler.Link `json:"items"`
		Page       int            `json:"page"`
		Size       int            `json:"size"`
		Total      int            `json:"total"`
		TotalPages int            `json:"total_pages"`
	}
	if err := json.Unmarshal(rrPage2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}

	if page2.Page != 2 || page2.Size != 20 || page2.Total != 30 || page2.TotalPages != 2 {
		t.Fatalf("unexpected pagination metadata: %+v", page2)
	}
	if len(page2.Items) != 10 {
		t.Fatalf("expected 10 items on second page, got %d", len(page2.Items))
	}

	rrFilter := doAuthedRequest(t, router, http.MethodGet, "/api/links?filter=alpha&size=1000", "", "", sid)
	if rrFilter.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rrFilter.Code)
	}

	var filtered struct {
		Items  []handler.Link `json:"items"`
		Size   int            `json:"size"`
		Filter string         `json:"filter"`
	}
	if err := json.Unmarshal(rrFilter.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode filter response: %v", err)
	}

	if filtered.Size != 100 {
		t.Fatalf("expected size cap at 100, got %d", filtered.Size)
	}
	if filtered.Filter != "alpha" {
		t.Fatalf("expected filter to echo, got %q", filtered.Filter)
	}
	if len(filtered.Items) != 10 {
		t.Fatalf("expected 10 filtered items, got %d", len(filtered.Items))
	}
}

func TestAdminUserManagementPermissions(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	userID := mustCreateUser(t, db, "member", "password123", false)
	adminSID := mustCreateSession(t, db, adminID)
	userSID := mustCreateSession(t, db, userID)

	createPayload := `{"username":"newuser","password":"password123","is_admin":true}`

	rrForbidden := doAuthedRequest(t, router, http.MethodPost, "/api/users", createPayload, "application/json", userSID)
	if rrForbidden.Code != http.StatusForbidden {
		b, _ := io.ReadAll(rrForbidden.Body)
		t.Fatalf("expected 403 for non-admin create user, got %d body=%s", rrForbidden.Code, string(b))
	}

	rrCreated := doAuthedRequest(t, router, http.MethodPost, "/api/users", createPayload, "application/json", adminSID)
	if rrCreated.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin create user, got %d", rrCreated.Code)
	}

	var newUserID int
	if err := db.QueryRow(`SELECT id FROM users WHERE username = ?`, "newuser").Scan(&newUserID); err != nil {
		t.Fatalf("query new user: %v", err)
	}

	rrToggle := doAuthedRequest(t, router, http.MethodPatch, fmt.Sprintf("/api/users/%d/admin", newUserID), `{"is_admin":false}`, "application/json", adminSID)
	if rrToggle.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin toggle, got %d", rrToggle.Code)
	}

	var isAdmin bool
	if err := db.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, newUserID).Scan(&isAdmin); err != nil {
		t.Fatalf("query admin state: %v", err)
	}
	if isAdmin {
		t.Fatal("expected user admin flag to be false after toggle")
	}

	rrDeleteSelf := doAuthedRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/users/%d", adminID), "", "", adminSID)
	if rrDeleteSelf.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when admin deletes own account, got %d", rrDeleteSelf.Code)
	}

	rrDeleteOther := doAuthedRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/users/%d", userID), "", "", adminSID)
	if rrDeleteOther.Code != http.StatusOK {
		t.Fatalf("expected 200 when admin deletes another user, got %d", rrDeleteOther.Code)
	}
}

func TestAdminCreateUserMultipartForm(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	adminSID := mustCreateSession(t, db, adminID)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("username", "formuser"); err != nil {
		t.Fatalf("write username field: %v", err)
	}
	if err := w.WriteField("password", "password123"); err != nil {
		t.Fatalf("write password field: %v", err)
	}
	if err := w.WriteField("is_admin", "on"); err != nil {
		t.Fatalf("write is_admin field: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Origin", "http://localhost:8080")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: adminSID})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 for multipart create, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSameOriginProtection(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	adminSID := mustCreateSession(t, db, adminID)

	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"xuser","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: adminSID})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin write, got %d", rr.Code)
	}
}

func TestCurrentUserEndpoint(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	adminSID := mustCreateSession(t, db, adminID)

	rr := doAuthedRequest(t, router, http.MethodGet, "/api/me", "", "", adminSID)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var me struct {
		ID      int    `json:"id"`
		User    string `json:"username"`
		IsAdmin bool   `json:"is_admin"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /api/me: %v", err)
	}
	if !me.IsAdmin || me.User != "admin" {
		t.Fatalf("unexpected /api/me payload: %+v", me)
	}
}

func TestAdminCanDeleteLinksOnly(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	memberID := mustCreateUser(t, db, "member", "password123", false)
	adminSID := mustCreateSession(t, db, adminID)
	memberSID := mustCreateSession(t, db, memberID)

	res, err := db.Exec(`INSERT INTO links (slug, url) VALUES (?, ?)`, "sample01", "https://example.com")
	if err != nil {
		t.Fatalf("insert link: %v", err)
	}
	linkID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	rrForbidden := doAuthedRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/links/%d", linkID), "", "", memberSID)
	if rrForbidden.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin delete, got %d", rrForbidden.Code)
	}

	rrAdmin := doAuthedRequest(t, router, http.MethodDelete, fmt.Sprintf("/api/links/%d", linkID), "", "", adminSID)
	if rrAdmin.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin delete, got %d", rrAdmin.Code)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM links WHERE id = ?`, linkID).Scan(&count); err != nil {
		t.Fatalf("query link count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected link to be deleted, remaining count=%d", count)
	}
}

func TestRedirectWritesAnalyticsEventsAndTopLinks(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	adminSID := mustCreateSession(t, db, adminID)

	if _, err := db.Exec(`INSERT INTO links (slug, url) VALUES (?, ?)`, "alpha01", "https://example.com/a"); err != nil {
		t.Fatalf("insert alpha link: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO links (slug, url) VALUES (?, ?)`, "beta01", "https://example.com/b"); err != nil {
		t.Fatalf("insert beta link: %v", err)
	}

	for range 3 {
		req := httptest.NewRequest(http.MethodGet, "/alpha01", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusFound {
			t.Fatalf("expected 302 for alpha redirect, got %d", rr.Code)
		}
	}

	reqBeta := httptest.NewRequest(http.MethodGet, "/beta01", nil)
	rrBeta := httptest.NewRecorder()
	router.ServeHTTP(rrBeta, reqBeta)
	if rrBeta.Code != http.StatusFound {
		t.Fatalf("expected 302 for beta redirect, got %d", rrBeta.Code)
	}

	rrAnalytics := doAuthedRequest(t, router, http.MethodGet, "/api/analytics", "", "", adminSID)
	if rrAnalytics.Code != http.StatusOK {
		t.Fatalf("expected 200 for analytics, got %d body=%s", rrAnalytics.Code, rrAnalytics.Body.String())
	}

	var payload struct {
		Daily []struct {
			Date        string `json:"date"`
			AccessCount int    `json:"access_count"`
		} `json:"daily"`
		TopLinks []struct {
			Slug        string `json:"slug"`
			AccessCount int    `json:"access_count"`
		} `json:"top_links"`
	}
	if err := json.Unmarshal(rrAnalytics.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode analytics: %v", err)
	}
	if len(payload.TopLinks) < 2 {
		t.Fatalf("expected at least 2 top links, got %d", len(payload.TopLinks))
	}
	if payload.TopLinks[0].Slug != "alpha01" || payload.TopLinks[0].AccessCount != 3 {
		t.Fatalf("unexpected top link: %+v", payload.TopLinks[0])
	}

	foundToday := false
	for _, p := range payload.Daily {
		if p.AccessCount > 0 {
			foundToday = true
			break
		}
	}
	if !foundToday {
		t.Fatal("expected at least one non-zero daily analytics point")
	}
}

func TestAnalyticsFiltersAndDateValidation(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	adminID := mustCreateUser(t, db, "admin", "password123", true)
	adminSID := mustCreateSession(t, db, adminID)

	if _, err := db.Exec(`INSERT INTO links (slug, url) VALUES (?, ?)`, "focus01", "https://example.com/focus"); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO link_access_events (link_id) SELECT id FROM links WHERE slug = ?`, "focus01"); err != nil {
		t.Fatalf("insert access event: %v", err)
	}

	rrFiltered := doAuthedRequest(t, router, http.MethodGet, "/api/analytics?slug=focus01", "", "", adminSID)
	if rrFiltered.Code != http.StatusOK {
		t.Fatalf("expected 200 for filtered analytics, got %d", rrFiltered.Code)
	}

	var filtered struct {
		Slug     string `json:"slug"`
		TopLinks []struct {
			Slug string `json:"slug"`
		} `json:"top_links"`
	}
	if err := json.Unmarshal(rrFiltered.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode filtered analytics: %v", err)
	}
	if filtered.Slug != "focus01" {
		t.Fatalf("expected slug filter echo, got %q", filtered.Slug)
	}
	if len(filtered.TopLinks) == 0 || filtered.TopLinks[0].Slug != "focus01" {
		t.Fatalf("unexpected filtered top links: %+v", filtered.TopLinks)
	}

	rrInvalidDate := doAuthedRequest(t, router, http.MethodGet, "/api/analytics?start_date=2026-04-10&end_date=2026-04-01", "", "", adminSID)
	if rrInvalidDate.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date range, got %d", rrInvalidDate.Code)
	}
}

func TestMissingSlugRendersCustom404Page(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/no-such-slug", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown slug, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "custom 404") {
		t.Fatalf("expected custom 404 page body, got %q", rr.Body.String())
	}
}

func TestUnmatchedPathRendersCustom404Page(t *testing.T) {
	db, router := testRouter(t)
	defer db.Close()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/totally/missing/path", nil)
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unmatched path, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "custom 404") {
		t.Fatalf("expected custom 404 page body, got %q", rr.Body.String())
	}
}
