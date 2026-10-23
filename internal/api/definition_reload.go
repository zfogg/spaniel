package api

import (
	"github.com/zfogg/spaniel/internal/ws"
	"net/http"
	"time"
)

func (r *Router) reloadDashboardDefinitions(w http.ResponseWriter, q *http.Request) {
	var reload func() error
	if r.settings != nil {
		reload = r.settings.ReloadDashboards
	}
	r.reloadDefinitions(w, q, "dashboard", reload)
}

func (r *Router) reloadDefinitions(w http.ResponseWriter, q *http.Request, kind string, reload func() error) {
	if reload == nil {
		respondErr(w, q, http.StatusNotImplemented, kind+" directory reload is not configured")
		return
	}
	emit := func(status, message string) {
		if r.hub != nil {
			r.hub.Broadcast(&ws.Event{Type: "definition_sync", Timestamp: time.Now().UnixNano(), Payload: map[string]string{"kind": kind, "status": status, "message": message}})
		}
	}
	emit("updating", "")
	if err := reload(); err != nil {
		emit("error", err.Error())
		respondErr(w, q, http.StatusBadRequest, kind+" YAML reload failed: "+err.Error())
		return
	}
	emit("updated", "")
	respond(w, map[string]bool{"ok": true}, 1, 1)
}
