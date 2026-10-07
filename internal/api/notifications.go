package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (r *Router) listNotifications(w http.ResponseWriter, q *http.Request) {
	page, limit := positiveQueryInt(q, "page", 1), positiveQueryInt(q, "limit", 30)
	items, total, err := r.store.WithContext(q.Context()).ListNotifications(page, limit)
	if err != nil {
		respondErr(w, q, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, items, int(total), page)
}

func (r *Router) readNotification(w http.ResponseWriter, q *http.Request) {
	if err := r.store.WithContext(q.Context()).MarkNotificationRead(chi.URLParam(q, "id"), false); err != nil {
		respondErr(w, q, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}

func (r *Router) acknowledgeNotification(w http.ResponseWriter, q *http.Request) {
	if err := r.store.WithContext(q.Context()).MarkNotificationRead(chi.URLParam(q, "id"), true); err != nil {
		respondErr(w, q, http.StatusInternalServerError, err.Error())
		return
	}
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
