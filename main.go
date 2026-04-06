package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nv-9/lynx/internal/config"
	"github.com/nv-9/lynx/internal/database"
	"github.com/nv-9/lynx/internal/handler"
	"github.com/nv-9/lynx/internal/middleware"
)

//go:embed static
var staticFiles embed.FS

func main() {
	cfg := config.Load()

	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
	defer db.Close()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}

	h := handler.New(db, cfg.BaseURL, staticFS)

	r := chi.NewRouter()
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.EnforceSameOriginWrite(cfg.BaseURL))

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

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

	log.Printf("Lynx listening on :%s (base URL: %s)", cfg.Port, cfg.BaseURL)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
