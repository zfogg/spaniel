package client

// MergeByID replaces existing identities and appends new rows in arrival order.
// It is suitable for a bounded live cache; server sorting/pagination still owns
// authoritative HTTP snapshots. Callers supply resource-specific identities.
func MergeByID[T any](rows, incoming []T, limit int, id func(T) string) []T {
	if limit <= 0 {
		return nil
	}
	out := make([]T, 0, len(rows)+len(incoming))
	indexes := map[string]int{}
	for _, batch := range [][]T{rows, incoming} {
		for _, row := range batch {
			key := id(row)
			if i, ok := indexes[key]; ok {
				out[i] = row
			} else {
				indexes[key] = len(out)
				out = append(out, row)
			}
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
