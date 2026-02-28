#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/one-agent}"
BRANCH="${BRANCH:-main}"
RUN_TESTS="${RUN_TESTS:-1}"
APP_USER="${APP_USER:-shopping-agent}"

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
  if [[ "${RUN_TESTS}" == "1" ]]; then
    log "Running Go tests"
    env \
      -u BLINKIT_SEARCH_MODE \
      -u BLINKIT_BROWSER_WORKER_URL \
      -u BLINKIT_BROWSER_HELPER \
      -u BLINKIT_BROWSER_NODE \
      -u BLINKIT_BROWSER_TIMEOUT \
      -u BLINKIT_BROWSER_TIMEOUT_MS \
      go test ./...
  else
    log "Skipping tests (RUN_TESTS=${RUN_TESTS})"
  fi

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

prepare_runtime_dirs() {
  log "Preparing runtime directories"
  mkdir -p \
    "${APP_DIR}/data" \
    "${APP_DIR}/.data/blinkit-browser-profile" \
    "${APP_DIR}/.cache/ms-playwright"
  chown -R "${APP_USER}:${APP_USER}" "${APP_DIR}/data" "${APP_DIR}/.data" "${APP_DIR}/.cache"
}

install_systemd_units() {
  log "Installing systemd unit files"
  install -m 0644 "${APP_DIR}/deploy/hostinger/shopping-agent.service" /etc/systemd/system/shopping-agent.service
  install -m 0644 "${APP_DIR}/deploy/hostinger/blinkit-browser-worker.service" /etc/systemd/system/blinkit-browser-worker.service
  install -m 0644 "${APP_DIR}/deploy/hostinger/token-refresher.service" /etc/systemd/system/token-refresher.service
  install -m 0644 "${APP_DIR}/deploy/hostinger/token-refresher.timer" /etc/systemd/system/token-refresher.timer
}

restart_services() {
  log "Reloading systemd units"
  systemctl daemon-reload
  log "Restarting services"
  systemctl restart blinkit-browser-worker.service
  systemctl restart shopping-agent.service
  systemctl restart token-refresher.timer
}

health_check() {
  local attempts=10
  local delay=2
  local i
  log "Waiting for service"
  for ((i=1; i<=attempts; i++)); do
    if curl -fsS http://127.0.0.1:8080/health >/dev/null; then
      log "Deploy successful"
      return
    fi
    sleep "${delay}"
  done

  log "Health check failed after $((attempts * delay))s; collecting diagnostics"
  systemctl --no-pager --full status shopping-agent.service || true
  systemctl --no-pager --full status blinkit-browser-worker.service || true
  journalctl -u shopping-agent.service -n 80 --no-pager || true
  journalctl -u blinkit-browser-worker.service -n 80 --no-pager || true
  return 1
}

main() {
  require_root
  require_repo
  deploy_code
  build_binaries
  install_browser_deps
  prepare_runtime_dirs
  install_systemd_units
  restart_services
  health_check
}

main "$@"
