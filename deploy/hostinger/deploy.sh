#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/one-agent}"
BRANCH="${BRANCH:-main}"

log() {
  printf '[deploy] %s\n' "$*"
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    echo "Run as root: sudo ${APP_DIR}/deploy/hostinger/deploy.sh" >&2
    exit 1
  fi
}

require_repo() {
  if [[ ! -d "${APP_DIR}/.git" ]]; then
    echo "Git repo not found at ${APP_DIR}" >&2
    exit 1
  fi
  if [[ ! -f "${APP_DIR}/go.mod" ]]; then
    echo "go.mod not found at ${APP_DIR}" >&2
    exit 1
  fi
}

deploy_code() {
  cd "${APP_DIR}"
  log "Fetching latest code"
  git fetch origin "${BRANCH}"
  git checkout "${BRANCH}"
  git pull --ff-only origin "${BRANCH}"
}

build_binaries() {
  cd "${APP_DIR}"
  log "Running Go tests"
  go test ./...

  log "Building binaries"
  go build -o ./bin/server ./cmd/server
  go build -o ./bin/refresher ./cmd/refresher
  go build -o ./bin/proxyman-import ./cmd/proxyman-import
  go build -o ./bin/proxyman-watch ./cmd/proxyman-watch
}

install_browser_deps() {
  cd "${APP_DIR}"
  if [[ ! -d "${APP_DIR}/node_modules/playwright" ]]; then
    log "Installing Playwright"
    npm install --no-save playwright
  fi

  log "Ensuring Chromium is installed"
  PLAYWRIGHT_BROWSERS_PATH="${APP_DIR}/.cache/ms-playwright" npx playwright install chromium
}

restart_services() {
  log "Restarting services"
  systemctl restart blinkit-browser-worker.service
  systemctl restart shopping-agent.service
  systemctl restart token-refresher.timer
}

health_check() {
  log "Waiting for service"
  sleep 2
  log "Health check"
  curl -fsS http://127.0.0.1:8080/health >/dev/null
  log "Deploy successful"
}

main() {
  require_root
  require_repo
  deploy_code
  build_binaries
  install_browser_deps
  restart_services
  health_check
}

main "$@"
