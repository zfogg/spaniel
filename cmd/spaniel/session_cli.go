package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zfogg/spaniel/internal/storage"
)

// fetchSessions returns the full session list from the API.
func fetchSessions(apiBase string) ([]storage.Session, error) {
	resp, err := http.Get(apiBase + "/api/sessions") //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list sessions: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data []storage.Session `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}
	return envelope.Data, nil
}

// resolveSessionRefFull is the verbose counterpart to resolveSessionRef — it
// returns the whole Session record because callers need the current
// is_baseline flag to toggle it. id-match wins over label.
func resolveSessionRefFull(apiBase, ref string) (storage.Session, error) {
	if ref == "" {
		return storage.Session{}, fmt.Errorf("empty session ref")
	}
	sessions, err := fetchSessions(apiBase)
	if err != nil {
		return storage.Session{}, err
	}
	for _, s := range sessions {
		if s.ID == ref {
			return s, nil
		}
	}
	for _, s := range sessions {
		if s.Label == ref {
			return s, nil
		}
	}
	return storage.Session{}, fmt.Errorf("no session with id or label %q", ref)
}

// activeSessionRef returns the id of the currently-active session, if any.
func activeSessionRef(apiBase string) (storage.Session, error) {
	resp, err := http.Get(apiBase + "/api/sessions/active") //nolint:gosec
	if err != nil {
		return storage.Session{}, fmt.Errorf("connect to spaniel at %s: %w", apiBase, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return storage.Session{}, fmt.Errorf("active session: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return storage.Session{}, err
	}
	if envelope.Data.ID == "" {
		return storage.Session{}, fmt.Errorf("no active session — pass an id/label or start one with `spaniel session new`")
	}
	return resolveSessionRefFull(apiBase, envelope.Data.ID)
}

// sessionListData fetches the data the list TUI needs. Pulled out so tests
// can verify the HTTP call without driving a TUI.
func sessionListData(apiBase string) (sessions []storage.Session, activeID string, err error) {
	sessions, err = fetchSessions(apiBase)
	if err != nil {
		return nil, "", err
	}
	if s, e := activeSessionRef(apiBase); e == nil {
		activeID = s.ID
	}
	return sessions, activeID, nil
}

// sessionListTUI launches the interactive picker. Pre-fetches sessions and
// active id, then hands them to the model.
func sessionListTUI(apiBase string) error {
	sessions, activeID, err := sessionListData(apiBase)
	if err != nil {
		return err
	}
	return runSessionListTUI(sessions, activeID, apiBase)
}

func fmtAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ── activate ───────────────────────────────────────────────────────────────

func sessionActivate(apiBase, ref string, out io.Writer) error {
	s, err := resolveSessionRefFull(apiBase, ref)
	if err != nil {
		return err
	}
	resp, err := http.Post(apiBase+"/api/sessions/"+s.ID+"/activate", "application/json", nil) //nolint:gosec
	if err != nil {
		return fmt.Errorf("activate: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("activate: HTTP %d: %s", resp.StatusCode, string(body))
	}
	fmt.Fprintf(out, "activated session: %s (%s)\n", s.Label, shortID(s.ID))
	return nil
}

// ── baseline ───────────────────────────────────────────────────────────────

func sessionBaseline(apiBase, ref string, out io.Writer) error {
	var s storage.Session
	var err error
	if ref == "" {
		s, err = activeSessionRef(apiBase)
	} else {
		s, err = resolveSessionRefFull(apiBase, ref)
	}
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]bool{"is_baseline": !s.IsBaseline})
	resp, err := http.Post(apiBase+"/api/sessions/"+s.ID+"/baseline", "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return fmt.Errorf("toggle baseline: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("toggle baseline: HTTP %d: %s", resp.StatusCode, string(raw))
	}
	state := "marked baseline"
	if s.IsBaseline {
		state = "unmarked baseline"
	}
	fmt.Fprintf(out, "%s: %s (%s)\n", state, s.Label, shortID(s.ID))
	return nil
}

// ── delete ─────────────────────────────────────────────────────────────────

func sessionDelete(apiBase, ref string, out io.Writer) error {
	s, err := resolveSessionRefFull(apiBase, ref)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest(http.MethodDelete, apiBase+"/api/sessions/"+s.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == 400 {
		body, _ := io.ReadAll(resp.Body)
		var errEnv struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &errEnv)
		msg := strings.TrimSpace(errEnv.Error)
		if msg == "" {
			msg = string(body)
		}
		return fmt.Errorf("refused by server: %s", msg)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete: HTTP %d: %s", resp.StatusCode, string(body))
	}
	fmt.Fprintf(out, "deleted session: %s (%s)\n", s.Label, shortID(s.ID))
	return nil
}
