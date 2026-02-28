package blinkit

import (
	"strconv"

	"one-agent/internal/types"
)

func withSearchLocation(sess *types.AppSession, lat, lng float64) *types.AppSession {
	if sess == nil {
		return nil
	}
	if lat == 0 && lng == 0 {
		return sess
	}
	copyHeaders := make(map[string]string, len(sess.DeviceHeaders))
	for k, v := range sess.DeviceHeaders {
		copyHeaders[k] = v
	}
	if lat != 0 {
		s := formatCoord(lat)
		copyHeaders["lat"] = s
		copyHeaders["cur_lat"] = s
	}
	if lng != 0 {
		s := formatCoord(lng)
		copyHeaders["lon"] = s
		copyHeaders["cur_lon"] = s
	}
	out := *sess
	out.DeviceHeaders = copyHeaders
	return &out
}

func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
