package storage

import (
	"encoding/json"
	"strings"
)

// attachQueryOccurrences adds distinct execution counts for each span's own
// persisted session, service, fingerprint (including version/dialect), database
// namespace and server address/port. Missing identity attributes remain unknown;
// they are never guessed from the SQL. Retransmitted trace/span IDs count once.
func (d *DB) attachQueryOccurrences(spans []*Span) error {
	ids := make([]string, 0, len(spans))
	for _, s := range spans {
		var attrs map[string]any
		if json.Unmarshal([]byte(s.Attributes), &attrs) != nil {
			continue
		}
		fingerprint, _ := attrs["gotel.db.query.fingerprint"].(string)
		if strings.HasPrefix(fingerprint, "sequel:v1:") {
			ids = append(ids, s.SpanID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	counts, err := d.namedQuery("SessionQueryOccurrences").Span.QueryOccurrences(ids)
	if err != nil {
		return err
	}
	byID := map[string]int64{}
	for _, c := range counts {
		byID[c.TraceID+"/"+c.SpanID] = c.Count
	}
	for _, s := range spans {
		s.QueryOccurrences = byID[s.TraceID+"/"+s.SpanID]
	}
	return nil
}
