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

// fetchSessions returns the full session list from the API. Helper for
// the CLI list/activate/baseline/delete commands.
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

// resolveSessionRefFull is the verbose counterpart to resolveSessionRef
// (in diff.go) — returns the whole Session record because callers need
// the current is_baseline flag to toggle it. id-match wins over label.
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
// Used as the default for `spaniel session baseline` (no arg).
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
	// Pull the full record so the caller has is_baseline etc.
	return resolveSessionRefFull(apiBase, envelope.Data.ID)
}

// ── list ───────────────────────────────────────────────────────────────────

func sessionList(apiBase string, out io.Writer) error {
	sessions, err := fetchSessions(apiBase)
	if err != nil {
		return err
	}
	// Also fetch the active session so we can mark it with a `*`.
	activeID := ""
	if s, err := activeSessionRef(apiBase); err == nil {
		activeID = s.ID
	}
	if len(sessions) == 0 {
		fmt.Fprintln(out, "no sessions yet — start one with `spaniel session new`")
		return nil
	}
	tw := newColWriter(out)
	fmt.Fprintln(tw, "  \tID\tLABEL\tFLAGS\tTRACES\tSPANS\tAGE")
	now := time.Now()
	for _, s := range sessions {
		marker := " "
		if s.ID == activeID {
			marker = "*"
		}
		flags := []string{}
		if s.IsBaseline {
			flags = append(flags, "baseline")
		}
		if s.IsImported {
			flags = append(flags, "imported")
		}
		flagStr := strings.Join(flags, ",")
		if flagStr == "" {
			flagStr = "-"
		}
		age := now.Sub(time.Unix(0, s.CreatedAt))
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			marker, shortID(s.ID), s.Label, flagStr,
			s.TraceCount, s.SpanCount, fmtAge(age))
	}
	tw.flush()
	return nil
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

// sessionBaseline toggles is_baseline on a session. If ref is empty the
// active session is used. The server expects {is_baseline: bool} as the
// new desired value, so we read the current flag and POST the inverse.
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
		// Server's friendly refusal — usually "cannot delete the active session".
		body, _ := io.ReadAll(resp.Body)
		var errEnv struct{ Error string `json:"error"` }
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
