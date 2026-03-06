package mcp

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const blinkitLoginTargetURL = "https://blinkit.com/"

var blinkitLoginPortalTemplate = template.Must(template.New("blinkit-login-portal").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  {{if .Refresh}}<meta http-equiv="refresh" content="5">{{end}}
  <title>Blinkit Login</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #101415;
      --panel: #172021;
      --text: #f3f7f5;
      --muted: #a9b5b1;
      --accent: #d7ff64;
      --danger: #ff8b7b;
      --warn: #ffd76a;
      --ok: #7af0a0;
      --border: rgba(255,255,255,0.12);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      padding: 24px;
      background:
        radial-gradient(circle at top left, rgba(215,255,100,0.16), transparent 28%),
        linear-gradient(180deg, #0d1112 0%, var(--bg) 100%);
      color: var(--text);
      font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    .wrap {
      max-width: 760px;
      margin: 0 auto;
      background: color-mix(in srgb, var(--panel) 88%, black);
      border: 1px solid var(--border);
      border-radius: 20px;
      padding: 24px;
      backdrop-filter: blur(16px);
      box-shadow: 0 18px 60px rgba(0,0,0,0.35);
    }
    h1 { margin: 0 0 8px; font-size: 28px; line-height: 1.15; }
    p { margin: 0; color: var(--muted); }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 10px;
      margin: 18px 0 10px;
      padding: 10px 14px;
      border-radius: 999px;
      border: 1px solid var(--border);
      background: rgba(255,255,255,0.04);
      font-weight: 600;
    }
    .status.pending { color: var(--warn); }
    .status.completed { color: var(--ok); }
    .status.failed { color: var(--danger); }
    .card {
      margin-top: 18px;
      padding: 18px;
      border-radius: 16px;
      border: 1px solid var(--border);
      background: rgba(255,255,255,0.03);
    }
    .button {
      display: inline-block;
      margin-top: 14px;
      padding: 12px 16px;
      border-radius: 12px;
      background: var(--accent);
      color: #101415;
      font-weight: 700;
      text-decoration: none;
    }
    .meta {
      margin-top: 16px;
      color: var(--muted);
      font-size: 14px;
    }
    code {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 13px;
      color: var(--text);
      word-break: break-word;
    }
  </style>
</head>
<body>
  <main class="wrap">
    <h1>Blinkit Login</h1>
    <p>Use this page from your phone to open the remote browser session tied to your One-Agent worker profile.</p>
    <div class="status {{.Status}}">Status: {{.Status}}</div>
    <div class="card">
      <p>{{.Message}}</p>
      {{if .WorkerMessage}}<p style="margin-top:12px;">Worker: {{.WorkerMessage}}</p>{{end}}
      {{if .BrowserURL}}
      <a class="button" href="{{.BrowserURL}}">Open Remote Browser</a>
      {{else if .StatusPending}}
      <p style="margin-top:12px;">Remote browser URL is not configured on the server yet.</p>
      {{end}}
      <div class="meta">
        <div>Login ID: <code>{{.LoginID}}</code></div>
        <div>Expires At: <code>{{.ExpiresAt}}</code></div>
        <div>Refresh this page after completing login if it does not auto-update.</div>
      </div>
    </div>
  </main>
</body>
</html>`))

type loginPortalView struct {
	LoginID       string
	Status        string
	StatusPending bool
	Message       string
	WorkerMessage string
	BrowserURL    string
	ExpiresAt     string
	Refresh       bool
}

func blinkitStartLoginURL(loginID string) string {
	portalURL := blinkitLoginPortalURL(loginID)
	browserURL := blinkitExternalLoginURL(loginID)
	switch {
	case portalURL != "" && browserURL != "":
		return portalURL
	case browserURL != "":
		return browserURL
	default:
		return ""
	}
}

func blinkitLoginPortalURL(loginID string) string {
	base := strings.TrimSpace(os.Getenv("MCP_PUBLIC_BASE_URL"))
	if base == "" || loginID == "" {
		return ""
	}
	values := url.Values{}
	values.Set("login_id", loginID)
	return strings.TrimRight(base, "/") + "/login/blinkit?" + values.Encode()
}

func blinkitExternalLoginURL(loginID string) string {
	templateURL := strings.TrimSpace(os.Getenv("BLINKIT_EXTERNAL_LOGIN_URL_TEMPLATE"))
	if templateURL != "" {
		return renderLoginURLTemplate(templateURL, loginID)
	}
	return strings.TrimSpace(os.Getenv("BLINKIT_EXTERNAL_LOGIN_URL"))
}

func renderLoginURLTemplate(templateURL, loginID string) string {
	replacer := strings.NewReplacer(
		"{login_id}", loginID,
		"{login_id_escaped}", url.QueryEscape(loginID),
		"{app}", "blinkit",
		"{app_escaped}", url.QueryEscape("blinkit"),
		"{target_url}", blinkitLoginTargetURL,
		"{target_url_escaped}", url.QueryEscape(blinkitLoginTargetURL),
	)
	return replacer.Replace(templateURL)
}

func (s *Server) handleLoginPortal(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/login/blinkit" {
		return false
	}
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return true
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return true
	}

	loginID := strings.TrimSpace(r.URL.Query().Get("login_id"))
	if loginID == "" {
		writeLoginPortalHTML(w, http.StatusBadRequest, missingLoginPortalView())
		return true
	}

	status, found := s.loginPortalStatus(r, loginID)
	if !found {
		writeLoginPortalHTML(w, http.StatusNotFound, expiredLoginPortalView(loginID))
		return true
	}
	writeLoginPortalHTML(w, http.StatusOK, loginPortalViewFromStatus(status))
	return true
}

func writeLoginPortalHTML(w http.ResponseWriter, statusCode int, view loginPortalView) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Ingest-Secret")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = blinkitLoginPortalTemplate.Execute(w, view)
}

func missingLoginPortalView() loginPortalView {
	return loginPortalView{
		Status:  loginStatusFailed,
		Message: "Missing login_id query parameter",
	}
}

func expiredLoginPortalView(loginID string) loginPortalView {
	return loginPortalView{
		LoginID:   loginID,
		Status:    loginStatusFailed,
		Message:   "Login flow not found or has expired",
		ExpiresAt: "",
	}
}

func (s *Server) loginPortalStatus(r *http.Request, loginID string) (loginStatusOutput, bool) {
	status, ok := s.loginFlows.get(loginID)
	if !ok || status.App != "blinkit" {
		return loginStatusOutput{}, false
	}
	worker := s.fetchBlinkitWorkerStatus(r.Context(), defaultBlinkitWorkerStatusTimeout)
	status.Worker = &worker
	if status.Status == loginStatusPending && worker.ChallengeDetected {
		status.NeedsHumanVerification = true
	}
	return status, true
}

func loginPortalViewFromStatus(status loginStatusOutput) loginPortalView {
	view := loginPortalView{
		LoginID:       status.LoginID,
		Status:        status.Status,
		StatusPending: status.Status == loginStatusPending,
		Message:       status.Message,
		BrowserURL:    blinkitExternalLoginURL(status.LoginID),
		ExpiresAt:     status.ExpiresAt.UTC().Format("2006-01-02 15:04:05 MST"),
		Refresh:       status.Status == loginStatusPending,
	}
	if status.Worker != nil {
		view.WorkerMessage = status.Worker.Message
	}
	return view
}
