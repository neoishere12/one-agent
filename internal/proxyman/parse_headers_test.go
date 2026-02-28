package proxyman

import (
	"bytes"
	"testing"

	"one-agent/internal/types"
)

func TestParseHARSkipsVolatileTransportHeaders(t *testing.T) {
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://api2.grofers.com/v1/layout/search", []harHeader{
			{Name: "access_token", Value: "access-header"},
			{Name: "auth_key", Value: "refresh-header"},
			{Name: "req_key", Value: "req-old"},
			{Name: "session_uuid", Value: "sess-old"},
			{Name: "Cookie", Value: "__cf_bm=bm-value; path=/; expires=Fri, 27-Feb-26 08:26:49 GMT; domain=.grofers.com; _cfuvid=uv-value"},
			{Name: "Host", Value: "api2.grofers.com"},
			{Name: "Connection", Value: "keep-alive"},
			{Name: "Content-Length", Value: "999"},
			{Name: "X-Device-Id", Value: "dev-123"},
			{Name: "User-Agent", Value: "blinkit/1.0"},
		}, map[string]any{"ok": true}),
	})), ParseOptions{App: types.PlatformBlinkit})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if payload.DeviceHeaders["cookie"] != "__cf_bm=bm-value; _cfuvid=uv-value" {
		t.Fatalf("cookie should be normalized, got %q", payload.DeviceHeaders["cookie"])
	}
	if payload.DeviceHeaders["host"] != "" {
		t.Fatalf("host should be excluded, got %q", payload.DeviceHeaders["host"])
	}
	if payload.DeviceHeaders["connection"] != "" {
		t.Fatalf("connection should be excluded, got %q", payload.DeviceHeaders["connection"])
	}
	if payload.DeviceHeaders["content-length"] != "" {
		t.Fatalf("content-length should be excluded, got %q", payload.DeviceHeaders["content-length"])
	}
	if payload.DeviceHeaders["req_key"] != "req-old" {
		t.Fatalf("req_key should be kept, got %q", payload.DeviceHeaders["req_key"])
	}
	if payload.DeviceHeaders["session_uuid"] != "sess-old" {
		t.Fatalf("session_uuid should be kept, got %q", payload.DeviceHeaders["session_uuid"])
	}
	if payload.DeviceHeaders["x-device-id"] != "dev-123" {
		t.Fatalf("x-device-id missing, got %+v", payload.DeviceHeaders)
	}
}

func TestParseHARPrefersLatestBlinkitSearchHeaderSnapshot(t *testing.T) {
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, blinkitHeaderSnapshotEntries())), ParseOptions{App: types.PlatformBlinkit})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if got := payload.DeviceHeaders["session_uuid"]; got != "sess-from-search-2" {
		t.Fatalf("session_uuid should come from latest search snapshot, got %q", got)
	}
	if got := payload.DeviceHeaders["req_key"]; got != "req-from-search-2" {
		t.Fatalf("req_key should come from latest search snapshot, got %q", got)
	}
	if got := payload.DeviceHeaders["app_version"]; got != "185111" {
		t.Fatalf("app_version should come from latest search snapshot, got %q", got)
	}
}

func blinkitHeaderSnapshotEntries() []harEntry {
	return []harEntry{
		{
			StartedDateTime: "2026-02-27T10:00:00Z",
			Request: harRequest{
				Method: "POST",
				URL:    "https://api2.grofers.com/v1/layout/search?q=milk&search_type=type_to_search&",
				Headers: []harHeader{
					{Name: "access_token", Value: "access-header"},
					{Name: "auth_key", Value: "refresh-header"},
					{Name: "req_key", Value: "req-from-search-1"},
					{Name: "session_uuid", Value: "sess-from-search-1"},
					{Name: "app_version", Value: "185110"},
				},
			},
			Response: harResponse{Status: 200, Content: harContent{Text: `{"ok":true}`, MimeType: "application/json"}},
		},
		{
			StartedDateTime: "2026-02-27T10:00:30Z",
			Request: harRequest{
				Method: "POST",
				URL:    "https://api2.grofers.com/v1/aerobar",
				Headers: []harHeader{
					{Name: "session_uuid", Value: "sess-from-other-route"},
					{Name: "req_key", Value: "req-from-other-route"},
				},
			},
			Response: harResponse{Status: 200, Content: harContent{Text: `{"ok":true}`, MimeType: "application/json"}},
		},
		{
			StartedDateTime: "2026-02-27T10:01:00Z",
			Request: harRequest{
				Method: "POST",
				URL:    "https://api2.grofers.com/v1/layout/search?q=milk%202&search_type=type_to_search&",
				Headers: []harHeader{
					{Name: "req_key", Value: "req-from-search-2"},
					{Name: "session_uuid", Value: "sess-from-search-2"},
					{Name: "app_version", Value: "185111"},
				},
			},
			Response: harResponse{Status: 200, Content: harContent{Text: `{"ok":true}`, MimeType: "application/json"}},
		},
	}
}

func TestParseHARKeepsLatestDeviceHeaderByEntryTime(t *testing.T) {
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		{
			StartedDateTime: "2026-02-27T10:59:31.706Z",
			Request: harRequest{
				Method: "POST",
				URL:    "https://api2.grofers.com/v1/layout/search",
				Headers: []harHeader{
					{Name: "access_token", Value: "access-header"},
					{Name: "auth_key", Value: "refresh-header"},
					{Name: "app_version", Value: "latest-app-version"},
				},
			},
			Response: harResponse{Status: 200, Content: harContent{Text: `{"ok":true}`, MimeType: "application/json"}},
		},
		{
			StartedDateTime: "2026-02-27T08:01:07.187Z",
			Request: harRequest{
				Method: "POST",
				URL:    "https://api2.grofers.com/v1/layout/search",
				Headers: []harHeader{
					{Name: "access_token", Value: "access-header"},
					{Name: "auth_key", Value: "refresh-header"},
					{Name: "app_version", Value: "old-app-version"},
				},
			},
			Response: harResponse{Status: 200, Content: harContent{Text: `{"ok":true}`, MimeType: "application/json"}},
		},
	})), ParseOptions{App: types.PlatformBlinkit})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if got := payload.DeviceHeaders["app_version"]; got != "latest-app-version" {
		t.Fatalf("app_version should keep latest value, got %q", got)
	}
}

func TestParseHARKeepsBlinkitWebHeaders(t *testing.T) {
	payload, err := ParseHAR(bytes.NewReader(mustHAR(t, []harEntry{
		harFixtureEntry("https://blinkit.com/v1/layout/search?q=amul%20lassi&search_type=type_to_search", []harHeader{
			{Name: "access_token", Value: "access-header"},
			{Name: "auth_key", Value: "refresh-header"},
			{Name: "app_client", Value: "consumer_web"},
			{Name: "platform", Value: "mobile_web"},
			{Name: "web_app_version", Value: "1008010016"},
			{Name: "origin", Value: "https://blinkit.com"},
			{Name: "referer", Value: "https://blinkit.com/s/?q=amul%20lassi"},
		}, map[string]any{"ok": true}),
	})), ParseOptions{App: types.PlatformBlinkit})
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if got := payload.DeviceHeaders["app_client"]; got != "consumer_web" {
		t.Fatalf("app_client: got %q", got)
	}
	if got := payload.DeviceHeaders["platform"]; got != "mobile_web" {
		t.Fatalf("platform: got %q", got)
	}
	if got := payload.DeviceHeaders["web_app_version"]; got != "1008010016" {
		t.Fatalf("web_app_version: got %q", got)
	}
	if got := payload.DeviceHeaders["origin"]; got != "https://blinkit.com" {
		t.Fatalf("origin: got %q", got)
	}
	if got := payload.DeviceHeaders["referer"]; got == "" {
		t.Fatal("referer should be retained for web sessions")
	}
}
