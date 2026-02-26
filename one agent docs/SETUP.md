# SETUP.md

> One-time setup guide. Run these steps once. Never again (unless VPS is rebuilt).
> Total time: ~20 minutes.

---

## Prerequisites

- Ubuntu 24.04 VPS (Hetzner CX11 or equivalent — €3–5/month recommended)
- SSH access to VPS
- iPhone with WireGuard app installed (free, App Store)
- Blinkit, Zepto, Swiggy Instamart installed on iPhone and logged in

---

## Step 1 — VPS Initial Setup

```bash
ssh root@<your-vps-ip>

# Update packages
apt update && apt upgrade -y

# Install dependencies
apt install -y wireguard sqlite3 curl git

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

# Generate master encryption key
openssl rand -hex 32
# Copy the output — this is your STORE_MASTER_KEY

# Create environment file (root-only permissions)
cat > /etc/shopping-agent.env << EOF
STORE_MASTER_KEY=<paste-32-byte-hex-here>
MCP_PORT=8080
PROXY_PORT=8888
WIREGUARD_INTERFACE=wg0
TOKEN_REFRESH_INTERVAL=4h
EOF
chmod 600 /etc/shopping-agent.env
```

---

## Step 3 — WireGuard Server Setup

```bash
# Generate server keys
wg genkey | tee /etc/wireguard/server_private | wg pubkey > /etc/wireguard/server_public
cat /etc/wireguard/server_public  # save this — you need it for iPhone config

# Create wg0.conf (leave [Peer] empty for now — add iPhone key in Step 4)
cat > /etc/wireguard/wg0.conf << EOF
[Interface]
PrivateKey = $(cat /etc/wireguard/server_private)
Address = 10.0.0.1/24
ListenPort = 51820
PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE

# iPhone peer added in Step 4
EOF

# Enable IP forwarding
echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf
sysctl -p

# Open firewall port
ufw allow 51820/udp
ufw allow 8080/tcp   # MCP server
ufw allow 8888/tcp   # MITM proxy (only during setup)

# Start WireGuard
systemctl enable --now wg-quick@wg0
```

---

## Step 4 — WireGuard iPhone Setup

1. Open **WireGuard** app on iPhone
2. Tap **+** → **Create from scratch**
3. Give it a name: `shopping-agent-setup`
4. Under **Interface**, tap **Generate keypair**
5. Copy the **Public Key** — you need it for VPS
6. Fill in:
   - **Addresses:** `10.0.0.2/32`
   - **DNS:** `1.1.1.1`
7. Under **Peers**, add:
   - **Public Key:** `<VPS server_public key from Step 3>`
   - **Endpoint:** `<your-vps-ip>:51820`
   - **Allowed IPs:** `10.0.0.1/32` (only route VPS IP through tunnel)

**Back on VPS** — add iPhone as peer:
```bash
# Add to /etc/wireguard/wg0.conf:
cat >> /etc/wireguard/wg0.conf << EOF

[Peer]
PublicKey = <iphone-public-key-from-step-4>
AllowedIPs = 10.0.0.2/32
EOF

# Apply config
wg syncconf wg0 <(wg-quick strip wg0)
```

**Test:** Enable WireGuard on iPhone, then `ping 10.0.0.1` — should get responses.

---

## Step 5 — Generate Root CA Certificate

```bash
cd /opt/shopping-agent
go run ./cmd/server gencert
# Outputs: certs/ca.crt, certs/ca.key
# ca.key never leaves the VPS
```

---

## Step 6 — Install CA on iPhone

```bash
# Serve ca.crt temporarily over HTTP for easy download
cd /opt/shopping-agent/certs
python3 -m http.server 9999 &
# VPS firewall: ufw allow 9999/tcp

# On iPhone (WireGuard tunnel active):
# Open Safari → http://10.0.0.1:9999/ca.crt
# Tap "Allow" to download profile

# iPhone: Settings → General → VPN & Device Management
# → Tap the downloaded profile → Install

# iPhone: Settings → General → About → Certificate Trust Settings
# → Enable full trust for your CA name

# Stop the temp HTTP server
kill %1
ufw delete allow 9999/tcp
```

---

## Step 7 — Register systemd Services

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

## Step 8 — Capture Sessions (one per app)

For each app (Blinkit, Zepto, Instamart):

1. On iPhone: **Enable WireGuard tunnel** (`shopping-agent-setup`)
2. On iPhone: **Wi-Fi settings** → your network → **Configure Proxy** → Manual → `10.0.0.1:8888`
3. Tell Claude: `"Capture my Blinkit session"`
4. Claude calls `capture_session("blinkit")` — server starts MITM proxy
5. On iPhone: **Open Blinkit**, browse for 30 seconds (search something, view cart, visit addresses)
6. Claude confirms capture complete
7. Repeat for Zepto and Instamart (~2 min each)

---

## Step 9 — Disable WireGuard

1. iPhone: **Wi-Fi settings** → remove HTTP proxy config
2. iPhone: **Disable WireGuard tunnel**

You no longer need WireGuard. The VPS runs silently. All future orders go through Claude.

---

## Verify Setup

```bash
# Check MCP server is running
curl https://<your-vps-ip>:8080/health

# List captured sessions (via Claude)
# Claude: "List my sessions"
```

---

## Maintenance

| Task | How Often | How |
|---|---|---|
| Token refresh | Automatic (every 4h) | systemd timer |
| Re-capture if session expires | When `token_valid: false` | Steps 8 above |
| Update Go binary | When features change | `git pull && go build && systemctl restart shopping-agent` |
| Backup token store | Automated (daily) | Configured in `shopping-agent.service` |

---

*See also: [TOKENS.md](TOKENS.md) for token lifecycle | [ARCHITECTURE.md](ARCHITECTURE.md) for system overview*
