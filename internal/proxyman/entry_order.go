package proxyman

import (
	"sort"
	"time"
)

type indexedEntry struct {
	idx int
	at  time.Time
	ok  bool
	e   harEntry
}

func orderedEntries(entries []harEntry) []harEntry {
	if len(entries) < 2 {
		return entries
	}
	indexed := make([]indexedEntry, 0, len(entries))
	for i, e := range entries {
		t, ok := parseEntryTime(e.StartedDateTime)
		indexed = append(indexed, indexedEntry{idx: i, at: t, ok: ok, e: e})
	}
	sort.SliceStable(indexed, func(i, j int) bool {
		a, b := indexed[i], indexed[j]
		switch {
		case a.ok && b.ok:
			return a.at.Before(b.at)
		case a.ok && !b.ok:
			return true
		case !a.ok && b.ok:
			return false
		default:
			return a.idx < b.idx
		}
	})
	out := make([]harEntry, 0, len(indexed))
	for _, row := range indexed {
		out = append(out, row.e)
	}
	return out
}

func parseEntryTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
