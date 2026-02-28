package proxyman

import (
	"time"

	"one-agent/internal/types"
)

// IngestPayload matches the JSON body accepted by POST /sessions/ingest.
type IngestPayload struct {
	App            string            `json:"app"`
	AccessToken    string            `json:"access_token"`
	RefreshToken   string            `json:"refresh_token"`
	TokenExpiresAt time.Time         `json:"token_expires_at"`
	DeviceHeaders  map[string]string `json:"device_headers"`
	Addresses      []IngestAddress   `json:"address_ids"`
	Payments       []IngestPayment   `json:"payment_tokens"`
}

// IngestAddress is a captured address row sent to the ingest endpoint.
type IngestAddress struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	FullAddress string `json:"full_address"`
}

// IngestPayment is a captured payment instrument sent to the ingest endpoint.
type IngestPayment struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Token string `json:"token"`
	Type  string `json:"type"`
}

// ParseOptions customizes HAR parsing behavior.
type ParseOptions struct {
	App             types.Platform
	Now             func() time.Time
	TokenExpiresAt  time.Time
	DefaultTokenTTL time.Duration
}

type harFile struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Entries []harEntry `json:"entries"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
}

type harRequest struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers []harHeader `json:"headers"`
}

type harResponse struct {
	Status  int        `json:"status"`
	Content harContent `json:"content"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harContent struct {
	Text     string `json:"text"`
	Encoding string `json:"encoding"`
	MimeType string `json:"mimeType"`
}

type aggregate struct {
	accessToken          string
	refreshToken         string
	expiresAt            time.Time
	deviceHeaders        map[string]string
	blinkitSearchHeaders map[string]string
	addresses            map[string]IngestAddress
	payments             map[string]IngestPayment
}

type authFields struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}
