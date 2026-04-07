package handler

import "net/http"

func (h *Handler) NotFoundPage(w http.ResponseWriter, r *http.Request) {
	h.serveHTMLWithStatus(w, "404.html", http.StatusNotFound)
}

func (h *Handler) PrivacyPage(w http.ResponseWriter, r *http.Request) {
	h.serveHTML(w, "privacy.html")
}
