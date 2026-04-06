package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io/fs"
	mathrand "math/rand"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	db      *sql.DB
	baseURL string
	static  fs.FS
}

func New(db *sql.DB, baseURL string, static fs.FS) *Handler {
	return &Handler{
		db:      db,
		baseURL: strings.TrimRight(baseURL, "/"),
		static:  static,
	}
}

const slugChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateSlug() string {
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 6)
	for i := range b {
		b[i] = slugChars[r.Intn(len(slugChars))]
	}
	return string(b)
}

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) serveHTML(w http.ResponseWriter, name string) {
	h.serveHTMLWithStatus(w, name, http.StatusOK)
}

func (h *Handler) serveHTMLWithStatus(w http.ResponseWriter, name string, status int) {
	data, err := fs.ReadFile(h.static, name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}
