#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/one-agent}"
APP_USER="${APP_USER:-shopping-agent}"
ENV_FILE="${ENV_FILE:-/etc/shopping-agent.env}"
GO_VERSION="${GO_VERSION:-1.24.0}"
NODE_MAJOR="${NODE_MAJOR:-20}"

log() {
  printf '[hostinger-bootstrap] %s\n' "$*"
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "Run as root: sudo bash deploy/hostinger/bootstrap.sh" >&2
    exit 1
  fi
}

require_repo() {
  if [[ ! -f "${APP_DIR}/go.mod" ]]; then
    echo "go.mod not found under ${APP_DIR}" >&2
    echo "Clone repo to ${APP_DIR} first, then rerun." >&2
    exit 1
  fi
}

install_base_packages() {
  log "Installing base packages"
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    build-essential \
    pkg-config \
    unzip \
    jq \
    ufw \
    openssl
}

install_node() {
  local need_install=1
  if command -v node >/dev/null 2>&1; then
    local current_major
    current_major="$(node -v | sed -E 's/^v([0-9]+).*/\1/')"
    if [[ "${current_major}" == "${NODE_MAJOR}" ]]; then
      need_install=0
    fi
  fi

  if [[ "${need_install}" -eq 1 ]]; then
    log "Installing Node.js ${NODE_MAJOR}.x"
    curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash -
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends nodejs
  else
    log "Node.js ${NODE_MAJOR}.x already present"
  fi
}

install_go() {
  local arch go_arch tarball url current_go
  arch="$(dpkg --print-architecture)"
  case "${arch}" in
    amd64) go_arch="amd64" ;;
    arm64) go_arch="arm64" ;;
    *)
      echo "Unsupported architecture for Go install: ${arch}" >&2
      exit 1
      ;;
  esac

  current_go=""
  if command -v go >/dev/null 2>&1; then
    current_go="$(go version | awk '{print $3}' | sed 's/^go//')"
  fi

  if [[ "${current_go}" == "${GO_VERSION}" ]]; then
    log "Go ${GO_VERSION} already present"
    return
  fi

  log "Installing Go ${GO_VERSION}"
  tarball="go${GO_VERSION}.linux-${go_arch}.tar.gz"
  url="https://go.dev/dl/${tarball}"

  curl -fsSL "${url}" -o "/tmp/${tarball}"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "/tmp/${tarball}"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

  go version
}

ensure_app_user() {
  if id -u "${APP_USER}" >/dev/null 2>&1; then
    log "User ${APP_USER} already exists"
    return
  fi

  log "Creating system user ${APP_USER}"
  useradd \
    --system \
    --create-home \
    --home-dir "/var/lib/${APP_USER}" \
    --shell /usr/sbin/nologin \
    "${APP_USER}"
}

prepare_app_dirs() {
  log "Preparing app directories"
  mkdir -p \
    "${APP_DIR}/bin" \
    "${APP_DIR}/data" \
    "${APP_DIR}/.data/blinkit-browser-profile" \
    "${APP_DIR}/.cache/ms-playwright"
  chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}"
}

build_binaries() {
  log "Building Go binaries"
  cd "${APP_DIR}"
  /usr/local/go/bin/go build -o ./bin/server ./cmd/server
  /usr/local/go/bin/go build -o ./bin/refresher ./cmd/refresher
  /usr/local/go/bin/go build -o ./bin/proxyman-import ./cmd/proxyman-import
  /usr/local/go/bin/go build -o ./bin/proxyman-watch ./cmd/proxyman-watch
  chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}/bin"
}

install_playwright() {
  log "Installing Playwright + Chromium"
  cd "${APP_DIR}"
  npm install --no-save playwright
  PLAYWRIGHT_BROWSERS_PATH="${APP_DIR}/.cache/ms-playwright" npx playwright install --with-deps chromium
  chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}/node_modules" "${APP_DIR}/.cache"
}

create_env_file_if_missing() {
  if [[ -f "${ENV_FILE}" ]]; then
    log "Using existing env file ${ENV_FILE}"
    return
  fi

  log "Creating ${ENV_FILE}"
  local store_master_key ingest_secret
  store_master_key="$(openssl rand -hex 32)"
  ingest_secret="$(openssl rand -hex 32)"

  cat > "${ENV_FILE}" <<EOT
STORE_MASTER_KEY=${store_master_key}
INGEST_SECRET=${ingest_secret}

MCP_PORT=8080
TOKEN_REFRESH_INTERVAL=4h
DB_PATH=${APP_DIR}/data/sessions.db
INGEST_RATE_LIMIT_MAX=10
INGEST_RATE_LIMIT_WINDOW=1m

BLINKIT_SEARCH_MODE=browser
BLINKIT_BROWSER_WORKER_URL=http://127.0.0.1:42199
BLINKIT_BROWSER_TIMEOUT=120s
BLINKIT_BROWSER_PROFILE_DIR=${APP_DIR}/.data/blinkit-browser-profile
BLINKIT_BROWSER_HEADLESS=true
BLINKIT_BROWSER_TIMEOUT_MS=120000
BLINKIT_BROWSER_WORKER_PORT=42199
BLINKIT_BROWSER_WORKER_BIND=127.0.0.1
PLAYWRIGHT_BROWSERS_PATH=${APP_DIR}/.cache/ms-playwright
EOT

  chmod 600 "${ENV_FILE}"
  log "Created ${ENV_FILE}. Back it up securely before continuing."
}

install_systemd_units() {
  log "Installing systemd unit files"
  install -m 0644 "${APP_DIR}/deploy/hostinger/shopping-agent.service" /etc/systemd/system/shopping-agent.service
  install -m 0644 "${APP_DIR}/deploy/hostinger/blinkit-browser-worker.service" /etc/systemd/system/blinkit-browser-worker.service
  install -m 0644 "${APP_DIR}/deploy/hostinger/token-refresher.service" /etc/systemd/system/token-refresher.service
  install -m 0644 "${APP_DIR}/deploy/hostinger/token-refresher.timer" /etc/systemd/system/token-refresher.timer

  systemctl daemon-reload
  systemctl enable --now blinkit-browser-worker.service
  systemctl enable --now shopping-agent.service
  systemctl enable --now token-refresher.timer
}

open_firewall() {
  if command -v ufw >/dev/null 2>&1; then
    log "Opening firewall port 8080/tcp"
    ufw allow 8080/tcp >/dev/null 2>&1 || true
  fi
}

print_status() {
  log "Service status"
  systemctl --no-pager --full status blinkit-browser-worker.service | sed -n '1,20p'
  systemctl --no-pager --full status shopping-agent.service | sed -n '1,20p'
  systemctl --no-pager --full status token-refresher.timer | sed -n '1,20p'
  log "Health check: curl -sS http://127.0.0.1:8080/health"
}

main() {
  require_root
  require_repo
  install_base_packages
  install_node
  install_go
  ensure_app_user
  prepare_app_dirs
  build_binaries
  install_playwright
  create_env_file_if_missing
  install_systemd_units
  open_firewall
  print_status
}

main "$@"
