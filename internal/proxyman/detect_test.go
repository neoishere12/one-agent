package proxyman

import (
	"bytes"
	"testing"
)

func TestDetectAppFromName(t *testing.T) {
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{name: "api2.grofers.com_1.har", want: "blinkit", ok: true},
		{name: "zepto_capture.har", want: "zepto", ok: true},
		{name: "swiggy_instamart.har", want: "instamart", ok: true},
		{name: "unknown.har", want: "", ok: false},
	}
	for _, tc := range tests {
		got, ok := DetectAppFromName(tc.name)
		if ok != tc.ok || string(got) != tc.want {
			t.Fatalf("DetectAppFromName(%q): got (%q,%t) want (%q,%t)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDetectAppFromHAR(t *testing.T) {
	app, ok, err := DetectAppFromHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api2.grofers.com/v1/aerobar", nil, map[string]any{"ok": true}),
		harFixtureEntry("https://api2.grofers.com/v1/layout/search", nil, map[string]any{"ok": true}),
		harFixtureEntry("https://api2.grofers.com/v1/actions/auto_suggest", nil, map[string]any{"ok": true}),
	})))
	if err != nil {
		t.Fatalf("DetectAppFromHAR: %v", err)
	}
	if !ok || app != "blinkit" {
		t.Fatalf("DetectAppFromHAR: got (%q,%t)", app, ok)
	}
}

func TestDetectAppFromHARAmbiguous(t *testing.T) {
	app, ok, err := DetectAppFromHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api2.grofers.com/v1/aerobar", nil, map[string]any{"ok": true}),
		harFixtureEntry("https://api.zepto.com/v2/search", nil, map[string]any{"ok": true}),
	})))
	if err != nil {
		t.Fatalf("DetectAppFromHAR: %v", err)
	}
	if ok || app != "" {
		t.Fatalf("DetectAppFromHAR ambiguous should return not found, got (%q,%t)", app, ok)
	}
}

func TestDetectAppFromHARDecodeError(t *testing.T) {
	_, _, err := DetectAppFromHAR(bytes.NewReader([]byte("not-json")))
	if err == nil {
		t.Fatal("expected decode error")
	}
}
