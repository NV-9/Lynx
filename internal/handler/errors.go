package handler

import "net/http"

func (h *Handler) NotFoundPage(w http.ResponseWriter, r *http.Request) {
	h.serveHTMLWithStatus(w, "404.html", http.StatusNotFound)
}
