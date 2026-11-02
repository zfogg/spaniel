package alerts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
)

var heading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))

func clean(s string) string {
	// Never let server strings introduce terminal controls, OSC hyperlinks or
	// bidirectional display overrides. Newlines are preserved for wrapped detail.
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}
func oneLine(s string) string { return strings.Join(strings.Fields(clean(s)), " ") }
func label(s string) string {
	if s == "" {
		return "all"
	}
	return s
}
func stamp(ns int64) string {
	if ns == 0 {
		return "never"
	}
	return time.Unix(0, ns).UTC().Format("2006-01-02 15:04:05Z")
}
func tone(state string) string {
	color := "245"
	switch state {
	case "firing", "error":
		color = "203"
	case "pending":
		color = "214"
	case "resolved":
		color = "114"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(state)
}
func (m *model) status(name string) string {
	s := m.loads[name]
	if s.err != nil {
		suffix := ""
		if s.loaded {
			suffix = " (showing stale data)"
		}
		return "Error loading " + name + ": " + oneLine(s.err.Error()) + suffix
	}
	if !s.loaded {
		return "Loading " + name + "…"
	}
	if s.pending {
		return "Refreshing " + name + "… (snapshot may be stale)"
	}
	return ""
}
func (m *model) View() string {
	var head, body []string
	title := "Alert rules · read-only"
	if m.id != "" {
		title = "Alert · " + oneLine(m.rule.Name) + " · read-only"
	}
	head = append(head, heading.Render(title))
	if !m.connection.Connected {
		head = append(head, "Offline / connecting · retained snapshots are stale")
	}
	if m.id == "" {
		head = append(head, fmt.Sprintf("API page %d/%d · %d rules · state %s · severity %s (filters on this page)", m.page, max(1, (m.total+pageSize-1)/pageSize), m.total, label(m.state), label(m.severity)), "Search: "+oneLine(m.search))
		if status := m.status("list"); status != "" {
			head = append(head, status)
		}
		rows := m.visibleRules()
		body = append(body, "STATE / SEVERITY    RULE · GROUPED INSTANCES")
		for _, r := range rows {
			counts := make(map[string]int)
			for _, i := range r.Instances {
				counts[instanceState(apigen.AlertInstance(i))]++
			}
			var summary []string
			for _, s := range states[1:] {
				if counts[s] > 0 {
					summary = append(summary, fmt.Sprintf("%d %s", counts[s], s))
				}
			}
			text := fmt.Sprintf("%s / %s  %s · %d instances", ruleState(r), oneLine(string(r.Severity)), oneLine(r.Name), len(r.Instances))
			if len(summary) > 0 {
				text += " (" + strings.Join(summary, ", ") + ")"
			}
			body = append(body, marker(r.Id == m.selected)+text)
		}
		if len(rows) == 0 && m.loads["list"].loaded {
			body = append(body, "No alert rules match this page. Clear filters (c), search (/), or change API page ([ ]).")
		}
		body = m.selectedWindow(body, m.selected, ruleIDs(rows), 1)
	} else {
		head = append(head, "d definition · i instances · h history · l links", "Viewing: "+m.section)
		if m.deleted() {
			head = append(head, "Rule deleted or unavailable (404) · retained definition is stale · Escape returns")
		} else if status := m.status("rule"); status != "" {
			head = append(head, status)
		}
		switch m.section {
		case "definition":
			body = m.definition()
			body = m.scroll(body)
		case "instances":
			head = append(head, fmt.Sprintf("state %s · search %s", label(m.state), oneLine(m.search)))
			body = m.instanceLines()
		case "instance":
			body = m.scroll(m.instanceDetail())
		case "event":
			body = m.scroll(m.eventDetail())
		case "history":
			head = append(head, fmt.Sprintf("API page %d/%d · %d events · group %s", m.page, max(1, (m.total+pageSize-1)/pageSize), m.total, oneLine(label(m.group))), fmt.Sprintf("state %s · severity %s · search %s", label(m.hstate), label(m.hseverity), oneLine(m.hsearch)))
			if status := m.status("history"); status != "" {
				head = append(head, status)
			}
			body = m.historyLines()
		case "links":
			body = m.linkLines()
		}
	}
	if m.notice != "" {
		head = append(head, m.notice)
	}
	if m.editing {
		head = append(head, "Search> "+oneLine(string(m.draft))+"_  Enter apply · Esc cancel · Ctrl+U clear")
	}
	help := "↑↓ select · Enter details · / search · s state · v severity · [ ] API page · c clear"
	if m.id != "" {
		help = "↑↓ / PgUp PgDn · Enter inspect · / search · s state · v severity · [ ] history page · g group · t telemetry · c clear · Esc Back"
	}
	// Clip every rendered line, including statuses/help, to the current viewport.
	// This also works at one-column/one-row sizes without negative dimensions.
	if m.height < 10 && len(head) > 1 {
		head = head[:1]
	}
	available := max(0, m.height-len(head)-1)
	if len(head) >= m.height {
		head = head[:max(0, m.height-1)]
	}
	if len(body) > available {
		body = body[:available]
	}
	lines := append(head, body...)
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, help)
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "…")
	}
	return strings.Join(lines, "\n")
}
func marker(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}
func ruleIDs(rows []apigen.AlertRule) []string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.Id)
	}
	return ids
}
func (m *model) selectedWindow(lines []string, selected string, ids []string, prefix int) []string {
	// Reserve enough space for headings, help and the selected row's metadata.
	count := max(1, m.height-10)
	index := 0
	for i, id := range ids {
		if id == selected {
			index = i
			break
		}
	}
	start := max(0, index-count+1)
	end := min(len(lines), prefix+start+count)
	if prefix > len(lines) {
		return lines
	}
	result := append([]string{}, lines[:prefix]...)
	result = append(result, lines[prefix+start:end]...)
	return result
}
func (m *model) wrap(s string) []string {
	return strings.Split(ansi.Hardwrap(clean(s), max(1, m.width-1), true), "\n")
}
func (m *model) scroll(lines []string) []string {
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, m.wrap(line)...)
	}
	start := min(m.offset, max(0, len(wrapped)-max(1, m.height-8)))
	return wrapped[start:]
}
func (m *model) definition() []string {
	if !m.loads["rule"].loaded {
		return nil
	}
	r := m.rule
	lines := []string{tone(ruleState(r)) + " · " + string(r.Severity) + " · id " + oneLine(r.Id),
		"Condition / threshold: " + clean(r.ConditionJson), "Query (version " + fmt.Sprint(r.QueryVersion) + "):", clean(r.QuerySql),
		"Group by: " + clean(r.GroupByJson),
		fmt.Sprintf("Pending %s · cooldown %s · repeat %s", time.Duration(r.PendingForNs), time.Duration(r.CooldownNs), time.Duration(r.RepeatIntervalNs)),
		"Last evaluation: " + stamp(r.LastEvaluatedAt) + " · duration " + time.Duration(r.LastDurationNs).String(),
		"Last success: " + stamp(r.LastSuccessAt) + " · next: " + stamp(r.NextEvaluationAt),
		fmt.Sprintf("Notifications: browser=%t · pushover=%t", r.BrowserEnabled, r.PushoverEnabled)}
	if r.LastError != "" {
		lines = append(lines, "Evaluation error: "+clean(r.LastError))
	}
	if r.SourceFile != "" {
		lines = append(lines, fmt.Sprintf("Source: %s · modified=%t · updated=%t", clean(r.SourceFile), r.SourceFileModified, r.Updated != nil && *r.Updated))
	}
	if r.InstanceDiscoverySql != "" {
		lines = append(lines, "Instance discovery: "+clean(r.InstanceDiscoverySql), fmt.Sprintf("Every %s · stale after %s · last run %s", time.Duration(r.InstanceDiscoveryIntervalNs), time.Duration(r.InstanceDiscoveryStaleAfterNs), stamp(r.InstanceDiscoveryLastRunAt)))
	}
	lines = append(lines, "Annotations: "+clean(r.AnnotationsJson))
	lines = append(lines, m.silenceLines()...)
	return lines
}
func (m *model) silenceLines() []string {
	if s := m.status("silences"); s != "" {
		return []string{s}
	}
	lines := []string{fmt.Sprintf("Silences: %d server records (read-only)", len(m.silences))}
	for _, s := range m.silences {
		lines = append(lines, fmt.Sprintf("  group %s · %s → %s · %s", oneLine(label(s.GroupKey)), stamp(s.StartsAt), stamp(s.EndsAt), oneLine(s.Comment)))
	}
	return lines
}
func (m *model) instanceLines() []string {
	rows := m.instances()
	lines := []string{"STATE / VALUE    GROUP"}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.GroupKey)
		ack := ""
		if r.AcknowledgedAt != nil {
			ack = " (condition " + oneLine(r.State) + "; acknowledged " + stamp(*r.AcknowledgedAt) + ")"
		}
		lines = append(lines, marker(r.GroupKey == m.instance)+instanceState(r)+" / "+value(r.Value)+"  "+oneLine(r.GroupKey)+ack)
	}
	if len(rows) == 0 {
		lines = append(lines, "No current instances match. Press c to clear filters.")
	}
	lines = m.selectedWindow(lines, m.instance, ids, 1)
	lines = append(lines, "Enter inspects full labels, evaluation and acknowledgment context.")
	return append(lines, m.silenceLines()...)
}
func (m *model) instanceDetail() []string {
	for _, raw := range m.rule.Instances {
		r := apigen.AlertInstance(raw)
		if r.GroupKey != m.instance {
			continue
		}
		lines := []string{"Group: " + clean(r.GroupKey), "State: " + instanceState(r) + " / condition: " + r.State + " / value: " + value(r.Value), "Labels: " + clean(r.LabelsJson), "Last evaluated: " + stamp(r.LastEvaluatedAt)}
		if r.FirstPendingAt != nil {
			lines = append(lines, "Pending since: "+stamp(*r.FirstPendingAt))
		}
		if r.FiredAt != nil {
			lines = append(lines, "Fired: "+stamp(*r.FiredAt))
		}
		if r.ResolvedAt != nil {
			lines = append(lines, "Resolved: "+stamp(*r.ResolvedAt))
		}
		if r.LastError != "" {
			lines = append(lines, "Error: "+clean(r.LastError))
		}
		if r.AcknowledgedAt != nil {
			lines = append(lines, "Acknowledgment: "+stamp(*r.AcknowledgedAt)+" / "+clean(r.AcknowledgementNote))
		}
		return append(lines, m.silenceLines()...)
	}
	return []string{"This instance is no longer present. Press i for current instances."}
}
func (m *model) eventDetail() []string {
	for _, r := range m.events {
		if r.Id == m.event {
			return []string{"Event: " + r.Id, "Kind: " + r.Kind + " / state: " + r.State + " / value: " + value(r.Value), "Group: " + r.GroupKey, "Time: " + stamp(r.CreatedAt), r.Detail}
		}
	}
	return []string{"This event is no longer on the current history page. Press h for history."}
}
func (m *model) historyLines() []string {
	lines := []string{"UTC TIME / KIND / STATE / VALUE / GROUP"}
	var ids []string
	var selected []string
	for _, r := range m.events {
		ids = append(ids, r.Id)
		lines = append(lines, marker(r.Id == m.event)+fmt.Sprintf("%s  %s / %s / %s  %s", stamp(r.CreatedAt), oneLine(r.Kind), oneLine(r.State), value(r.Value), oneLine(r.GroupKey)))
		if r.Id == m.event {
			selected = m.wrap("Event detail: " + r.Detail)
		}
	}
	if len(m.events) == 0 && m.loads["history"].loaded {
		lines = append(lines, "No event history matches these filters.")
	}
	lines = m.selectedWindow(lines, m.event, ids, 1)
	return append(lines, selected...)
}
func parseMap(s string) map[string]string {
	var v map[string]string
	_ = json.Unmarshal([]byte(s), &v)
	return v
}
func sortedKeys(v map[string]string) []string {
	var keys []string
	for key := range v {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
