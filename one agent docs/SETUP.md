# SETUP.md

> One-time setup guide. Run these steps once. Never again (unless VPS is rebuilt).
> Total time: ~15 minutes.

---

## Prerequisites

- Ubuntu 24.04 VPS (Hetzner CX11 or equivalent — €3–5/month recommended)
- SSH access to VPS
- iPhone with **Proxyman** app installed
- Blinkit, Zepto, Swiggy Instamart installed on iPhone and logged in

---

## Fast Path — Hostinger VPS

If you are deploying on Hostinger and want a single bootstrap flow (Go + Node + Playwright + systemd units), use:

- [HOSTINGER_VPS.md](HOSTINGER_VPS.md)
- Script: `deploy/hostinger/bootstrap.sh`

---

## Step 1 — VPS Initial Setup

```bash
ssh root@<your-vps-ip>

# Update packages
apt update && apt upgrade -y

# Install dependencies
apt install -y sqlite3 curl git

# Install Go 1.22+
wget https://go.dev/dl/go1.22.4.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.22.4.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
go version  # should print go1.22.x
```

---

## Step 2 — Deploy the Binary

```bash
# Clone repo
git clone https://github.com/nitin/shopping-agent /opt/shopping-agent
cd /opt/shopping-agent

# Build binaries
go build -o bin/server ./cmd/server
go build -o bin/refresher ./cmd/refresher
go build -o bin/proxyman-import ./cmd/proxyman-import
go build -o bin/proxyman-watch ./cmd/proxyman-watch

# Generate master encryption key
openssl rand -hex 32
# Copy the output — this is your STORE_MASTER_KEY

# Generate ingest secret (used by /sessions/ingest importer requests)
openssl rand -hex 32
# Copy the output — this is your INGEST_SECRET

# Create environment file (root-only permissions)
cat > /etc/shopping-agent.env << EOF
STORE_MASTER_KEY=<paste-32-byte-hex-here>
INGEST_SECRET=<paste-ingest-secret-here>
MCP_PORT=8080
TOKEN_REFRESH_INTERVAL=4h
# Optional: ingest hardening defaults (tune/disable temporarily while testing HAR imports)
# INGEST_RATE_LIMIT_MAX=10
# INGEST_RATE_LIMIT_WINDOW=1m
EOF
chmod 600 /etc/shopping-agent.env

# Open firewall port
ufw allow 8080/tcp   # MCP server + ingest endpoint
```

---

## Step 3 — Install Proxyman on iPhone

1. Install **Proxyman** from the App Store on iPhone
2. Open Proxyman and enable local VPN capture when prompted
3. Confirm you can see app traffic in Proxyman before proceeding

Proxyman handles HTTPS interception on-device. No WireGuard tunnel or custom iOS app is required.

---

## Step 4 — Install Proxyman CA Certificate on iPhone

Proxyman decrypts HTTPS only after its CA certificate is installed and trusted on the iPhone.

1. Open **Proxyman** on iPhone
2. Use Proxyman's certificate install flow (Certificate / SSL Proxying / Install Certificate)
3. iPhone: **Settings → General → VPN & Device Management**
   → Tap the downloaded profile → **Install**
4. iPhone: **Settings → General → About → Certificate Trust Settings**
   → Enable full trust for the **Proxyman** certificate

You only do this once.

---

## Step 5 — Register systemd Services

```bash
cp /opt/shopping-agent/deploy/shopping-agent.service /etc/systemd/system/
cp /opt/shopping-agent/deploy/token-refresher.service /etc/systemd/system/
cp /opt/shopping-agent/deploy/token-refresher.timer /etc/systemd/system/

systemctl daemon-reload
systemctl enable --now shopping-agent
systemctl enable --now token-refresher.timer

# Verify
systemctl status shopping-agent   # should be active (running)
systemctl list-timers              # should show token-refresher.timer
```

---

## Step 6 — Capture Sessions (one per app, ~30 seconds each)

For each app (Blinkit, Zepto, Instamart):

1. Open **Proxyman** on iPhone and start capture (VPN/interception ON)
2. Open the target app (Blinkit / Zepto / Instamart), browse for 30 seconds
   (search something, view cart, visit addresses)
3. In Proxyman, export the captured traffic as a **HAR** file
   (share to Files / AirDrop / Mac)
4. Import into the VPS using either workflow below.

### Option A — One-shot import (manual command per HAR)

On a machine with this repo clone and the HAR file:

```bash
export INGEST_SECRET=<same-secret-from-/etc/shopping-agent.env>
./bin/proxyman-import \
  -har /path/to/blinkit.har \
  -dry-run \
  -diagnostics

# If the dry-run summary/diagnostics look good, post the session:
./bin/proxyman-import \
  -har /path/to/blinkit.har \
  -ingest-url http://<your-vps-ip>:8080/sessions/ingest
```

`-app` is optional now; importer auto-detects app from filename/HAR host hints when possible.

### Option B — Automated folder watcher (recommended for repeated captures)

Run once and keep it open while exporting HAR files:

```bash
export INGEST_SECRET=<same-secret-from-/etc/shopping-agent.env>
./bin/proxyman-watch \
  -dir /path/to/har-drop \
  -ingest-url http://<your-vps-ip>:8080/sessions/ingest \
  -diagnostics
```

Then export each iPhone capture HAR into `/path/to/har-drop`.
Watcher behavior:
- Auto-detects app from filename/HAR hosts (override with `-app` if needed)
- Parses + posts to `/sessions/ingest`
- Moves successful files to `<dir>/processed/`
- Moves failed files to `<dir>/failed/` with an `.error.txt` reason file

`.proxymanv2` files are not supported; export as HAR.

### Capture confirmation

5. Confirm via Claude (`list_sessions`) or call `capture_session("blinkit")` before step 4 if you want Claude to wait for ingest completion.
6. Repeat for Zepto and Instamart.

Legacy manual example (explicit app):
   ```bash
   export INGEST_SECRET=<same-secret-from-/etc/shopping-agent.env>
   ./bin/proxyman-import \
     -har /path/to/blinkit.har \
     -app blinkit \
     -dry-run \
     -diagnostics

   # If the dry-run summary/diagnostics look good, post the session:
   ./bin/proxyman-import \
     -har /path/to/blinkit.har \
     -app blinkit \
     -ingest-url http://<your-vps-ip>:8080/sessions/ingest
   ```

**Tip:** Use `-diagnostics` during dry-run to see redacted parser coverage (entry counts, auth/header matches, top paths) without printing tokens.

**If you hit `429` while iterating HAR imports:** increase `INGEST_RATE_LIMIT_MAX` or set `INGEST_RATE_LIMIT_MAX=0` temporarily in `/etc/shopping-agent.env`, then `systemctl restart shopping-agent`.

---

## Step 7 — Disable Proxyman Capture

1. Open **Proxyman** on iPhone and turn off VPN/interception when you are done capturing

You no longer need Proxyman running. The VPS runs silently. All future orders go through Claude.

---

## Optional — Blinkit Browser-Backed Search (for persistent 403 fingerprint errors)

If Blinkit keeps returning `403` / `device fingerprint rejected` even after fresh HAR captures, switch Blinkit `search_product` to browser-backed mode.

This mode runs a Playwright helper (`scripts/blinkit-browser-search.mjs`) from the server process and reads live search payloads from `blinkit.com` in a persistent Chromium profile.

### 1) Install Playwright on the machine running `./bin/server`

```bash
cd /opt/shopping-agent   # or your local repo path
npm install --no-save playwright
npx playwright install chromium
```

### 2) Bootstrap a logged-in Blinkit browser profile once

```bash
cd /opt/one-agent   # or your local repo path
export BLINKIT_BROWSER_PROFILE_DIR=$PWD/.data/blinkit-browser-profile
export BLINKIT_BROWSER_HEADLESS=false
node scripts/blinkit-browser-search.mjs --bootstrap
```

A Chromium window opens. Complete Blinkit login/OTP once.

Optional (persist the Blinkit session into MCP store without HAR import):
```bash
curl -s http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"bootstrap_blinkit_web_session","arguments":{"timeout_seconds":300}}}'
```

### 3) Enable browser mode in server env

For direct shell runs:

```bash
cd /opt/one-agent   # or your local repo path
export BLINKIT_SEARCH_MODE=browser
export BLINKIT_BROWSER_HELPER=$PWD/scripts/blinkit-browser-search.mjs
export BLINKIT_BROWSER_NODE=$(which node)
export BLINKIT_BROWSER_TIMEOUT=45s
export BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT=5m
export BLINKIT_BROWSER_PROFILE_DIR=$PWD/.data/blinkit-browser-profile
export BLINKIT_BROWSER_HEADLESS=true
./bin/server
```

Recommended for stable background usage (no browser relaunch per search): run a persistent local browser worker and point MCP to it.

Terminal A (start once):
```bash
cd /opt/one-agent   # or your local repo path
export BLINKIT_BROWSER_PROFILE_DIR=$PWD/.data/blinkit-browser-profile
export BLINKIT_BROWSER_HEADLESS=false   # use true if your account works in headless
export BLINKIT_BROWSER_WORKER_PORT=42199
nohup node scripts/blinkit-browser-search.mjs --worker --worker-port 42199 > /tmp/blinkit-worker.log 2>&1 &
```

Terminal B (server):
```bash
cd /opt/one-agent   # or your local repo path
export BLINKIT_SEARCH_MODE=browser
export BLINKIT_BROWSER_WORKER_URL=http://127.0.0.1:42199
export BLINKIT_BROWSER_TIMEOUT=120s
export BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT=5m
./bin/server
```

Optional (VPS-only when datacenter egress is challenged):
```bash
export BLINKIT_BROWSER_PROXY_SERVER=http://<proxy-host>:<port>
export BLINKIT_BROWSER_PROXY_USERNAME=<username>
export BLINKIT_BROWSER_PROXY_PASSWORD=<password>
```

Worker health check:
```bash
curl -sS http://127.0.0.1:42199/health
```

For systemd, add the same vars to `/etc/shopping-agent.env`, then:

```bash
systemctl restart shopping-agent
```

`search_product` for Blinkit will now use browser-backed search. Other Blinkit flows (cart/checkout/order) remain API-based.

---

## Verify Setup

```bash
# Check MCP server is running
curl http://<your-vps-ip>:8080/health

# MCP protocol handshake (standards-style endpoint)
curl -s http://<your-vps-ip>:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"manual-test","version":"1.0.0"}}}'

# List exposed tools via MCP wrapper
curl -s http://<your-vps-ip>:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

# Optional: verify legacy SSE endpoint is reachable
curl -N http://<your-vps-ip>:8080/sse
```

Use `/mcp` as your primary connector URL.  
If a client requires legacy SSE transport, use `/sse` (it will advertise `/messages?sessionId=...`).

---

## Maintenance

| Task | How Often | How |
|---|---|---|
| Token refresh | Automatic (every 4h) | systemd timer |
| Re-capture if session expires | When `token_valid: false` | Steps 6 above |
| Update Go binary | When features change | `git pull && go build && systemctl restart shopping-agent` |
| Backup token store | Automated (daily) | Configured in `shopping-agent.service` |

---

*See also: [TOKENS.md](TOKENS.md) for token lifecycle | [ARCHITECTURE.md](ARCHITECTURE.md) for system overview*
