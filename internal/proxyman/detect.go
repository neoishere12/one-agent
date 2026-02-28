package proxyman

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"one-agent/internal/types"
)

type appHint struct {
	app      types.Platform
	keywords []string
}

var appHints = []appHint{
	{app: types.PlatformBlinkit, keywords: []string{"blinkit", "grofers"}},
	{app: types.PlatformZepto, keywords: []string{"zepto"}},
	{app: types.PlatformInstamart, keywords: []string{"instamart", "swiggy"}},
}

// DetectAppFromName tries to infer the app from a filename or free-form text.
func DetectAppFromName(name string) (types.Platform, bool) {
	return detectByText(strings.ToLower(strings.TrimSpace(name)))
}

// DetectAppFromHAR infers app from request hosts/paths inside a HAR file.
func DetectAppFromHAR(r io.Reader) (types.Platform, bool, error) {
	var file harFile
	if err := json.NewDecoder(r).Decode(&file); err != nil {
		return "", false, fmt.Errorf("decode HAR: %w", err)
	}
	counts := make(map[types.Platform]int)
	for _, entry := range file.Log.Entries {
		u, err := url.Parse(entry.Request.URL)
		if err != nil {
			continue
		}
		if app, ok := detectByText(strings.ToLower(u.Host + " " + u.Path)); ok {
			counts[app]++
		}
	}
	return pickDetectedApp(counts)
}

// DetectApp tries filename first, then HAR contents.
func DetectApp(name string, r io.Reader) (types.Platform, error) {
	if app, ok := DetectAppFromName(name); ok {
		return app, nil
	}
	app, ok, err := DetectAppFromHAR(r)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("unable to detect app from file name or HAR hosts")
	}
	return app, nil
}

func detectByText(text string) (types.Platform, bool) {
	for _, hint := range appHints {
		if hasAnyKeyword(text, hint.keywords) {
			return hint.app, true
		}
	}
	return "", false
}

func hasAnyKeyword(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func pickDetectedApp(counts map[types.Platform]int) (types.Platform, bool, error) {
	var best types.Platform
	maxCount := 0
	tie := false
	for app, count := range counts {
		if count > maxCount {
			best = app
			maxCount = count
			tie = false
			continue
		}
		if count == maxCount && count > 0 {
			tie = true
		}
	}
	if maxCount == 0 || tie {
		return "", false, nil
	}
	return best, true, nil
}
