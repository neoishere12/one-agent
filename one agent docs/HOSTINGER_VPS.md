# HOSTINGER_VPS.md

> Deploy one-agent on Hostinger VPS (Ubuntu) with MCP server + Blinkit browser worker running in background.

---

## 1) Clone and run bootstrap

```bash
ssh root@<your-hostinger-vps-ip>

apt update && apt install -y git

git clone <your-repo-url> /opt/one-agent
cd /opt/one-agent

bash deploy/hostinger/bootstrap.sh
```

What the script does:
- installs Go + Node
- builds `bin/server`, `bin/refresher`, importer binaries
- installs Playwright + Chromium
- creates `/etc/shopping-agent.env` (if missing)
- installs and starts systemd units:
  - `shopping-agent.service`
  - `blinkit-browser-worker.service`
  - `token-refresher.timer`

---

## 2) Verify services

```bash
systemctl status shopping-agent --no-pager
systemctl status blinkit-browser-worker --no-pager
systemctl status token-refresher.timer --no-pager

curl -sS http://127.0.0.1:8080/health
```

Expected health:

```json
{"status":"ok"}
```

---

## 3) Expose the MCP endpoint

### Option A (recommended): domain + reverse proxy
Use your own domain/subdomain with Nginx + TLS and route to `http://127.0.0.1:8080`.

### Option B (quick test): cloudflared tunnel

```bash
cloudflared tunnel --url http://127.0.0.1:8080 --protocol http2 --edge-ip-version 4
```

Then use:
- MCP URL: `https://<tunnel-domain>/mcp`
- Health URL: `https://<tunnel-domain>/health`

---

## 4) Connect Claude / ChatGPT MCP client

- Base URL should target `/mcp`
- Test handshake:

```bash
curl -sS https://<public-host>/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"manual-test","version":"1.0.0"}}}'
```

Optional: bootstrap Blinkit web session directly (no HAR import) after browser login profile is ready:

```bash
curl -sS https://<public-host>/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"bootstrap_blinkit_web_session","arguments":{"timeout_seconds":300}}}'
```

Worker diagnostics + recovery (recommended before retrying Blinkit search):

```bash
# status probe
curl -sS https://<public-host>/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"blinkit_worker_status","arguments":{"timeout_seconds":5}}}'

# reverify and persist browser session
curl -sS https://<public-host>/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"reverify_blinkit_session","arguments":{"timeout_seconds":300}}}'
```

---

## 5) Logs and restart commands

```bash
journalctl -u shopping-agent -f
journalctl -u blinkit-browser-worker -f

systemctl restart blinkit-browser-worker
systemctl restart shopping-agent
```

Manual deploy command (from VPS):

```bash
APP_DIR=/opt/one-agent BRANCH=main /opt/one-agent/deploy/hostinger/deploy.sh
```

Emergency bypass (skip tests for one run only):

```bash
RUN_TESTS=0 APP_DIR=/opt/one-agent BRANCH=main /opt/one-agent/deploy/hostinger/deploy.sh
```

---

## 6) Important env file

Location: `/etc/shopping-agent.env`

Contains:
- `STORE_MASTER_KEY`
- `INGEST_SECRET`
- MCP + ingest settings
- Blinkit browser worker settings

Back up this file securely.

---

## Notes

- This removes Mac popup dependency by running browser automation on VPS.
- Anti-bot checks can still happen depending on account/session risk scoring.
- If you already have `/etc/shopping-agent.env`, bootstrap will reuse it and not rotate secrets.

### VPS anti-bot challenge handling

If Blinkit returns `human_verification_required` or a `403` challenge page from worker search, your VPS egress is being challenged.

Use compliant mitigations:

1. Keep a real logged-in browser profile in `/opt/one-agent/.data/blinkit-browser-profile`.
2. Configure browser proxy egress (residential/business approved):

```bash
cat >> /etc/shopping-agent.env <<'EOF'
BLINKIT_BROWSER_PROXY_SERVER=http://<proxy-host>:<port>
BLINKIT_BROWSER_PROXY_USERNAME=<username>
BLINKIT_BROWSER_PROXY_PASSWORD=<password>
EOF

systemctl restart blinkit-browser-worker
systemctl restart shopping-agent
```

Do not keep placeholder values (`<proxy-host>`, `<username>`, `<password>`) in `/etc/shopping-agent.env`; replace them with real proxy credentials or remove these keys entirely.

3. Re-test worker directly:

```bash
curl -sS http://127.0.0.1:42199/status

curl -sS --max-time 80 http://127.0.0.1:42199/search \
  -H 'Content-Type: application/json' \
  -d '{"query":"amul lassi","timeout_seconds":35,"lat":18.6456,"lng":73.8852}'
```
