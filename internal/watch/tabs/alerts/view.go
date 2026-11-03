package alerts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/zfogg/spaniel/internal/api/apigen"
	"github.com/zfogg/spaniel/internal/tui"
	"github.com/zfogg/spaniel/internal/watch/terminal"
)

func clean(s string) string { return terminal.Multiline(s) }

func oneLine(s string) string { return strings.Join(strings.Fields(clean(s)), " ") }
func label(s string) string {
	if s == "" {
		return "all"
	}
	return s
}
func stamp(ns int64, now ...time.Time) string {
	if ns == 0 {
		return "never"
	}
	return terminal.Timestamp(ns, "2006-01-02 15:04:05 MST", time.UTC, terminal.BatchTime(now...))
}
func tone(state string) string {
	style := tui.ContentLabel
	switch state {
	case "firing", "error":
		style = tui.ContentError
	case "pending":
		style = tui.ContentWarning
	case "resolved":
		style = tui.ContentOK
	}
	return tui.RenderContent(style, oneLine(state))
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
	head = append(head, tui.RenderContent(tui.ContentTitle, title))
	if !m.connection.Connected {
		head = append(head, "Offline / connecting · retained snapshots are stale")
	}
	if m.id == "" {
		head = append(head, tui.RenderContent(tui.ContentContext, fmt.Sprintf("API page %d/%d · %d rules · state %s · severity %s (filters on this page)", m.page, max(1, (m.total+pageSize-1)/pageSize), m.total, oneLine(label(m.state)), oneLine(label(m.severity)))), tui.ContentPair("Search: ", oneLine(m.search)))
		if status := m.status("list"); status != "" {
			head = append(head, status)
		}
		body = m.ruleLines(ruleIDs(m.visibleRules()))
	} else {
		head = append(head, tui.ContentPair("Viewing: ", m.section))
		if m.deleted() {
			head = append(head, "Rule deleted or unavailable (404) · retained definition is stale")
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
			if status := m.status("history"); status != "" {
				head = append(head, status)
			}
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
		head = append(head, "Search> "+oneLine(string(m.draft))+"_")
	}
	// Clip every rendered line, including statuses/help, to the current viewport.
	// This also works at one-column/one-row sizes without negative dimensions.
	if m.height < 10 && len(head) > 1 {
		head = head[:1]
	}
	available := max(0, m.height-len(head))
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
		// Detail builders sanitize each payload before applying shared table styles.
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, max(1, m.width-1), true), "\n")...)
	}
	start := min(m.offset, max(0, len(wrapped)-max(1, m.height-8)))
	return wrapped[start:]
}
func (m *model) definition() []string {
	if !m.loads["rule"].loaded {
		return nil
	}
	r := m.rule
	lines := []string{tone(ruleState(r)) + tui.RenderContent(tui.ContentContext, " · "+oneLine(string(r.Severity))+" · id "+oneLine(r.Id)), tui.ContentPair("Name: ", clean(r.Name)), tui.RenderContent(tui.ContentTitle, "Condition / threshold:")}
	lines = append(lines, m.attributeTable(r.ConditionJson)...)
	lines = append(lines, tui.RenderContent(tui.ContentTitle, "Query (version "+fmt.Sprint(r.QueryVersion)+"):"), tui.RenderContent(tui.ContentValue, clean(r.QuerySql)), tui.RenderContent(tui.ContentTitle, "Group by:"))
	groups := r.GroupByJson
	if groups == "" {
		groups = "[]"
	}
	lines = append(lines, m.attributeTable(`{"group_by":`+groups+`}`)...)
	lines = append(lines, fmt.Sprintf("Pending %s · cooldown %s · repeat %s", duration(r.PendingForNs), duration(r.CooldownNs), duration(r.RepeatIntervalNs)),
		tui.ContentPair("Last evaluation: ", m.stamp(r.LastEvaluatedAt)+" · duration "+duration(r.LastDurationNs)),
		tui.ContentPair("Last success: ", m.stamp(r.LastSuccessAt)+" · next: "+m.stamp(r.NextEvaluationAt)),
		fmt.Sprintf("Notifications: browser=%t · pushover=%t", r.BrowserEnabled, r.PushoverEnabled))
	if r.LastError != "" {
		lines = append(lines, "Evaluation error: "+clean(r.LastError))
	}
	if r.SourceFile != "" {
		lines = append(lines, fmt.Sprintf("Source: %s · modified=%t · updated=%t", clean(r.SourceFile), r.SourceFileModified, r.Updated != nil && *r.Updated))
	}
	if r.InstanceDiscoverySql != "" {
		lines = append(lines, "Instance discovery: "+clean(r.InstanceDiscoverySql), fmt.Sprintf("Every %s · stale after %s · last run %s", duration(r.InstanceDiscoveryIntervalNs), duration(r.InstanceDiscoveryStaleAfterNs), m.stamp(r.InstanceDiscoveryLastRunAt)))
	}
	lines = append(lines, tui.RenderContent(tui.ContentTitle, "Annotations:"))
	lines = append(lines, m.attributeTable(r.AnnotationsJson)...)
	lines = append(lines, m.silenceLines()...)
	return lines
}
func (m *model) silenceLines() []string {
	var lines []string
	if s := m.status("silences"); s != "" {
		lines = append(lines, s)
	}
	if !m.loads["silences"].loaded {
		return lines
	}
	lines = append(lines, fmt.Sprintf("Silences: %d server records (read-only)", len(m.silences)))
	for _, s := range m.silences {
		lines = append(lines, fmt.Sprintf("  group %s · %s → %s · %s", oneLine(label(s.GroupKey)), m.stamp(s.StartsAt), m.stamp(s.EndsAt), oneLine(s.Comment)))
	}
	return lines
}
func (m *model) instanceLines() []string {
	instances := m.instances()
	columns := []tableColumn{{"GROUP", 0, false}}
	if m.width >= 22 {
		columns = []tableColumn{{"STATE", 10, true}, {"GROUP", 0, false}}
	}
	if m.width >= 40 {
		columns = []tableColumn{{"STATE", 12, true}, {"VALUE", 10, false}, {"GROUP", 0, false}}
	}
	if m.width >= 80 {
		columns = append(columns, tableColumn{"LAST EVAL (UTC)", 20, false})
	}
	var ids []string
	var rows [][]string
	for _, r := range instances {
		ids = append(ids, r.GroupKey)
		var cells []string
		for _, column := range columns {
			switch column.title {
			case "STATE":
				cells = append(cells, instanceState(r))
			case "VALUE":
				cells = append(cells, value(r.Value))
			case "GROUP":
				cells = append(cells, r.GroupKey)
			case "LAST EVAL (UTC)":
				cells = append(cells, m.stamp(r.LastEvaluatedAt))
			}
		}
		rows = append(rows, cells)
	}
	lines := m.table(columns, rows, ids, m.instance)
	if len(rows) == 0 {
		lines = append(lines, "No current instances match.")
	}
	lines = m.selectedWindow(lines, m.instance, ids, 1)
	return append(lines, m.silenceLines()...)
}
func (m *model) instanceDetail() []string {
	if !m.loads["rule"].loaded {
		return nil
	}
	for _, raw := range m.rule.Instances {
		r := apigen.AlertInstance(raw)
		if r.GroupKey != m.instance {
			continue
		}
		lines := []string{tui.ContentPair("Group: ", clean(r.GroupKey)), tui.RenderContent(tui.ContentLabel, "State: ") + tone(instanceState(r)) + " / condition: " + oneLine(r.State) + " / value: " + value(r.Value), tui.RenderContent(tui.ContentTitle, "Labels:")}
		lines = append(lines, m.attributeTable(r.LabelsJson)...)
		lines = append(lines, "Last evaluated: "+m.stamp(r.LastEvaluatedAt))

		if r.FirstPendingAt != nil {
			lines = append(lines, "Pending since: "+m.stamp(*r.FirstPendingAt))
		}
		if r.FiredAt != nil {
			lines = append(lines, "Fired: "+m.stamp(*r.FiredAt))
		}
		if r.ResolvedAt != nil {
			lines = append(lines, "Resolved: "+m.stamp(*r.ResolvedAt))
		}
		if r.LastError != "" {
			lines = append(lines, "Error: "+clean(r.LastError))
		}
		if r.AcknowledgedAt != nil {
			lines = append(lines, "Acknowledgment: "+m.stamp(*r.AcknowledgedAt)+" / "+clean(r.AcknowledgementNote))
		}
		return append(lines, m.silenceLines()...)
	}
	return []string{"This instance is no longer present. Press i for current instances."}
}
func (m *model) eventDetail() []string {
	if !m.loads["history"].loaded {
		return nil
	}
	for _, r := range m.events {
		if r.Id == m.event {
			return []string{tui.ContentPair("Event: ", oneLine(r.Id)), tui.ContentPair("Kind: ", oneLine(r.Kind)) + " / state: " + tone(r.State) + " / value: " + value(r.Value), tui.ContentPair("Group: ", clean(r.GroupKey)), tui.ContentPair("Time: ", m.stamp(r.CreatedAt)), tui.RenderContent(tui.ContentValue, clean(r.Detail))}
		}
	}
	return []string{"This event is no longer on the current history page. Press h for history."}
}
func (m *model) historyLines() []string {
	columns := []tableColumn{{"KIND", 0, false}}
	if m.width >= 30 {
		columns = []tableColumn{{"KIND", 12, false}, {"STATE", 10, true}, {"GROUP", 0, false}}
	}
	if m.width >= 50 {
		columns = []tableColumn{{"UTC TIME", 8, false}, {"KIND", 12, false}, {"STATE", 10, true}, {"GROUP", 0, false}}
	}
	if m.width >= 75 {
		columns = []tableColumn{{"TIME (UTC)", 20, false}, {"KIND", 16, false}, {"STATE", 12, true}, {"GROUP", 0, false}}
	}
	if m.width >= 100 {
		columns = append(columns, tableColumn{"VALUE", 10, false})
	}
	var ids []string
	var rows [][]string
	var selected []string
	for _, r := range m.events {
		ids = append(ids, r.Id)
		var cells []string
		for _, column := range columns {
			switch column.title {
			case "TIME (UTC)", "UTC TIME":
				text := m.stamp(r.CreatedAt)
				cells = append(cells, text)
			case "KIND":
				cells = append(cells, r.Kind)
			case "STATE":
				cells = append(cells, r.State)
			case "GROUP":
				cells = append(cells, r.GroupKey)
			case "VALUE":
				cells = append(cells, value(r.Value))
			}
		}
		rows = append(rows, cells)
		if r.Id == m.event {
			selected = m.wrap("Event detail: " + r.Detail)
		}
	}
	lines := m.table(columns, rows, ids, m.event)
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

func (m *model) stamp(ns int64) string { return stamp(ns, m.env.Now()) }
