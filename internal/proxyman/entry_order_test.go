package proxyman

import "testing"

func TestOrderedEntriesSortByStartedDateTime(t *testing.T) {
	in := []harEntry{
		{StartedDateTime: "2026-02-27T10:59:31.706Z", Request: harRequest{URL: "https://api2.grofers.com/new"}},
		{StartedDateTime: "2026-02-27T08:01:07.187Z", Request: harRequest{URL: "https://api2.grofers.com/old"}},
	}
	out := orderedEntries(in)
	if len(out) != 2 {
		t.Fatalf("ordered entries length: got %d", len(out))
	}
	if out[0].Request.URL != "https://api2.grofers.com/old" {
		t.Fatalf("first url: got %q", out[0].Request.URL)
	}
	if out[1].Request.URL != "https://api2.grofers.com/new" {
		t.Fatalf("second url: got %q", out[1].Request.URL)
	}
}
