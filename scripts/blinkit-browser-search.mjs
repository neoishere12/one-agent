#!/usr/bin/env node

import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import process from "node:process";

const DEFAULT_TIMEOUT_MS = 45000;
const DEFAULT_BOOTSTRAP_TIMEOUT_MS = 300000;
const DEFAULT_WORKER_PORT = 42199;
const DEFAULT_WORKER_BIND = "127.0.0.1";
const SEARCH_HOST_SUFFIX = "blinkit.com";
const SEARCH_PATH = "/v1/layout/search";

function parseArgs(argv) {
  const args = {
    query: "",
    lat: null,
    lng: null,
    worker: false,
    bootstrap: false,
    workerPort: null,
    bind: null,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const key = argv[i];
    const val = argv[i + 1];
    if (key === "--worker") {
      args.worker = true;
      continue;
    }
    if (key === "--bootstrap") {
      args.bootstrap = true;
      continue;
    }
    if (key === "--worker-port" && val) {
      args.workerPort = Number.parseInt(val, 10);
      i += 1;
      continue;
    }
    if (key === "--bind" && val) {
      args.bind = val;
      i += 1;
      continue;
    }
    if (key === "--query" && val) {
      args.query = val;
      i += 1;
      continue;
    }
    if (key === "--lat" && val) {
      args.lat = Number.parseFloat(val);
      i += 1;
      continue;
    }
    if (key === "--lng" && val) {
      args.lng = Number.parseFloat(val);
      i += 1;
    }
  }
  if (!args.worker && !args.bootstrap && !args.query.trim()) {
    throw new Error("missing required --query argument");
  }
  return args;
}

function parseBool(raw, fallback) {
  if (raw === undefined) {
    return fallback;
  }
  const value = String(raw).trim().toLowerCase();
  if (value === "1" || value === "true" || value === "yes") {
    return true;
  }
  if (value === "0" || value === "false" || value === "no") {
    return false;
  }
  return fallback;
}

function timeoutMs() {
  const raw = process.env.BLINKIT_BROWSER_TIMEOUT_MS;
  if (!raw) {
    return DEFAULT_TIMEOUT_MS;
  }
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed) || parsed <= 0) {
    return DEFAULT_TIMEOUT_MS;
  }
  return parsed;
}

function bootstrapTimeoutMs(body = {}) {
  const bodyValue = Number.parseInt(body.timeout_seconds, 10);
  if (Number.isFinite(bodyValue) && bodyValue > 0) {
    return bodyValue * 1000;
  }
  const raw = process.env.BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT_MS;
  if (!raw) {
    return DEFAULT_BOOTSTRAP_TIMEOUT_MS;
  }
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed) || parsed <= 0) {
    return DEFAULT_BOOTSTRAP_TIMEOUT_MS;
  }
  return parsed;
}

function searchTimeoutMs(body = {}) {
  const bodyValue = Number.parseInt(body.timeout_seconds, 10);
  if (Number.isFinite(bodyValue) && bodyValue > 0) {
    return bodyValue * 1000;
  }
  return timeoutMs();
}

function workerPort(args) {
  if (Number.isFinite(args.workerPort) && args.workerPort > 0) {
    return args.workerPort;
  }
  const raw = process.env.BLINKIT_BROWSER_WORKER_PORT;
  if (!raw) {
    return DEFAULT_WORKER_PORT;
  }
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed) || parsed <= 0) {
    return DEFAULT_WORKER_PORT;
  }
  return parsed;
}

function workerBind(args) {
  const value = args.bind || process.env.BLINKIT_BROWSER_WORKER_BIND;
  if (!value) {
    return DEFAULT_WORKER_BIND;
  }
  return String(value).trim() || DEFAULT_WORKER_BIND;
}

function debugEnabled() {
  return parseBool(process.env.BLINKIT_BROWSER_DEBUG, false);
}

function debugLog(message) {
  if (!debugEnabled()) {
    return;
  }
  process.stderr.write(`[blinkit-browser] ${message}\n`);
}

function newDeadline(timeout) {
  return Date.now() + timeout;
}

function remainingTimeout(deadline) {
  return Math.max(1, deadline - Date.now());
}

function isSearchResponse(response) {
  try {
    const url = new URL(response.url());
    if (!url.hostname.endsWith(SEARCH_HOST_SUFFIX)) {
      return false;
    }
    if (url.pathname !== SEARCH_PATH) {
      return false;
    }
    const method = response.request().method().toUpperCase();
    return method === "GET" || method === "POST";
  } catch {
    return false;
  }
}

function toOptionalNumber(value) {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value === "string") {
    const parsed = Number.parseFloat(value);
    if (Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return null;
}

async function waitForSearchFromUrl(page, targetUrl, timeout) {
  const pending = page.waitForResponse(isSearchResponse, { timeout }).catch(() => null);
  await page.goto(targetUrl, { waitUntil: "domcontentloaded", timeout });
  return pending;
}

async function waitForSearchFromHome(page, query, timeout) {
  const pending = page.waitForResponse(isSearchResponse, { timeout }).catch(() => null);
  await page.goto("https://blinkit.com/", { waitUntil: "domcontentloaded", timeout });

  const candidates = [
    'input[type="search"]',
    'input[placeholder*="Search"]',
    'input[placeholder*="search"]',
    'input[aria-label*="Search"]',
    'input[name="q"]',
  ];

  let input = null;
  for (const selector of candidates) {
    const locator = page.locator(selector).first();
    if ((await locator.count()) > 0) {
      input = locator;
      break;
    }
  }

  if (!input) {
    return pending;
  }

  await input.click({ timeout: 5000 });
  await input.fill(query, { timeout: 5000 });
  await page.keyboard.press("Enter");
  return pending;
}

async function fetchSearchViaPage(page, query, timeout) {
  const endpoint = `${SEARCH_PATH}?q=${encodeURIComponent(query)}`;
  const raw = await page.evaluate(
    async ({ endpoint, timeout }) => {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeout);
      try {
        const response = await fetch(endpoint, {
          method: "GET",
          credentials: "include",
          signal: controller.signal,
          headers: {
            accept: "application/json,text/plain,*/*",
          },
        });
        const text = await response.text();
        return {
          ok: response.ok,
          status: response.status,
          url: response.url,
          text,
        };
      } catch (err) {
        return {
          ok: false,
          status: 0,
          url: endpoint,
          text: "",
          error: err && err.message ? String(err.message) : String(err),
        };
      } finally {
        clearTimeout(timer);
      }
    },
    { endpoint, timeout }
  );

  if (raw.error) {
    throw new Error(`in-page fetch failed: ${raw.error}`);
  }
  if (!raw.ok) {
    throw new Error(`in-page fetch status ${raw.status} (${raw.url})`);
  }
  try {
    return {
      url: raw.url,
      status: raw.status,
      body: JSON.parse(raw.text),
    };
  } catch {
    throw new Error("in-page fetch returned non-JSON response");
  }
}

function print(payload) {
  process.stdout.write(`${JSON.stringify(payload)}\n`);
}

function cleanError(err) {
  if (!err) {
    return "unknown error";
  }
  const message = typeof err === "string" ? err : err.message || String(err);
  return message.replace(/\s+/g, " ").trim();
}

function writeJSON(res, status, payload) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(payload));
}

async function readJSONBody(req) {
  const chunks = [];
  for await (const chunk of req) {
    chunks.push(chunk);
  }
  const text = Buffer.concat(chunks).toString("utf8");
  if (!text.trim()) {
    return {};
  }
  return JSON.parse(text);
}

function enqueueTask(state, task) {
  const next = state.queue.then(task, task);
  state.queue = next.then(
    () => undefined,
    () => undefined,
  );
  return next;
}

async function loadChromium() {
  try {
    const mod = await import("playwright");
    return mod.chromium;
  } catch {
    // fall through
  }
  try {
    const mod = await import("playwright-core");
    return mod.chromium;
  } catch {
    throw new Error(
      "playwright is not installed (run: npm install playwright && npx playwright install chromium)"
    );
  }
}

async function launchBrowserSession(chromium, timeout) {
  const profileDir = path.resolve(
    process.env.BLINKIT_BROWSER_PROFILE_DIR || ".data/blinkit-browser-profile"
  );
  fs.mkdirSync(profileDir, { recursive: true });

  const headless = parseBool(process.env.BLINKIT_BROWSER_HEADLESS, true);
  const launchOptions = {
    headless,
    viewport: { width: 1280, height: 840 },
    locale: "en-IN",
    timezoneId: "Asia/Kolkata",
  };
  if (process.env.BLINKIT_BROWSER_EXECUTABLE_PATH) {
    launchOptions.executablePath = process.env.BLINKIT_BROWSER_EXECUTABLE_PATH;
  }

  const context = await chromium.launchPersistentContext(profileDir, launchOptions);
  context.setDefaultTimeout(timeout);
  context.setDefaultNavigationTimeout(timeout);
  const page = context.pages()[0] ?? (await context.newPage());
  return { context, page, headless };
}

async function applyGeolocation(context, lat, lng) {
  if (lat === null || lng === null) {
    return;
  }
  await context.grantPermissions(["geolocation"], { origin: "https://blinkit.com" });
  await context.setGeolocation({ latitude: lat, longitude: lng, accuracy: 50 });
}

async function runSearch(context, page, query, lat, lng, timeout) {
  const budget = splitSearchBudget(timeout);
  await applyGeolocation(context, lat, lng);
  const searchURL = `https://blinkit.com/s/?q=${encodeURIComponent(query)}`;

  let response = await waitForSearchFromUrl(page, searchURL, budget.directMs);
  if (!response) {
    debugLog("direct URL search response not observed, trying home-page fallback");
    response = await waitForSearchFromHome(page, query, budget.homeMs);
  }

  if (response) {
    debugLog(`search response captured status=${response.status()}`);
    const body = await response.json();
    return {
      ok: true,
      url: response.url(),
      status: response.status(),
      raw: body,
    };
  }

  debugLog("network interception miss; using in-page fetch fallback");
  const fallback = await fetchSearchViaPage(page, query, budget.fetchMs);
  return {
    ok: true,
    url: fallback.url,
    status: fallback.status,
    raw: fallback.body,
  };
}

function splitSearchBudget(totalMs) {
  const timeout = Number.isFinite(totalMs) && totalMs > 0 ? totalMs : DEFAULT_TIMEOUT_MS;
  const minPhaseMs = 1000;

  let directMs = Math.floor(timeout * 0.5);
  let homeMs = Math.floor(timeout * 0.3);
  let fetchMs = timeout - directMs - homeMs;

  if (directMs < minPhaseMs) {
    directMs = minPhaseMs;
  }
  if (homeMs < minPhaseMs) {
    homeMs = minPhaseMs;
  }
  if (fetchMs < minPhaseMs) {
    const deficit = minPhaseMs - fetchMs;
    const cutHome = Math.min(deficit, Math.max(0, homeMs - minPhaseMs));
    homeMs -= cutHome;
    const remaining = deficit - cutHome;
    const cutDirect = Math.min(remaining, Math.max(0, directMs - minPhaseMs));
    directMs -= cutDirect;
    fetchMs = timeout - directMs - homeMs;
    if (fetchMs < minPhaseMs) {
      fetchMs = minPhaseMs;
    }
  }

  return { directMs, homeMs, fetchMs };
}

function decodeMaybe(value) {
  if (typeof value !== "string") {
    return "";
  }
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function firstNonEmpty(...values) {
  for (const value of values) {
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return "";
}

function findTokenByKey(store, cookies, keys) {
  const normalized = new Set(keys.map((k) => k.toLowerCase()));
  for (const [key, value] of Object.entries(store)) {
    if (normalized.has(String(key).toLowerCase()) && String(value).trim()) {
      return String(value).trim();
    }
  }
  for (const cookie of cookies) {
    if (normalized.has(String(cookie.name).toLowerCase())) {
      const value = decodeMaybe(cookie.value);
      if (value.trim()) {
        return value.trim();
      }
    }
  }
  return "";
}

function findAccessTokenFallback(store, cookies) {
  const candidates = [];
  for (const value of Object.values(store)) {
    if (typeof value === "string") {
      candidates.push(value);
    }
  }
  for (const cookie of cookies) {
    candidates.push(decodeMaybe(cookie.value));
  }
  for (const value of candidates) {
    if (value.startsWith("v2::")) {
      return value;
    }
  }
  return "";
}

async function readSessionSnapshot(context, page) {
  const storage = await page.evaluate(() => {
    const out = {};
    for (let i = 0; i < window.localStorage.length; i += 1) {
      const key = window.localStorage.key(i);
      if (!key) {
        continue;
      }
      out[key] = window.localStorage.getItem(key) || "";
    }
    return {
      localStorage: out,
      userAgent: navigator.userAgent || "",
    };
  });
  const cookies = await context.cookies("https://blinkit.com");
  const localStorage = storage.localStorage || {};
  const accessToken = firstNonEmpty(
    findTokenByKey(localStorage, cookies, [
      "gr_1_accessToken",
      "access_token",
      "accessToken",
    ]),
    findAccessTokenFallback(localStorage, cookies),
  );
  const refreshToken = firstNonEmpty(
    findTokenByKey(localStorage, cookies, [
      "auth_key",
      "refresh_token",
      "refreshToken",
      "gr_1_authKey",
      "gr_1_refreshToken",
    ]),
    accessToken,
  );
  return {
    accessToken,
    refreshToken,
    userAgent: storage.userAgent || "",
  };
}

function buildBootstrapResult(snapshot) {
  const now = new Date();
  const expiresAt = new Date(now.getTime() + 7 * 24 * 60 * 60 * 1000);
  return {
    ok: true,
    session: {
      access_token: snapshot.accessToken,
      refresh_token: snapshot.refreshToken,
      token_expires_at: expiresAt.toISOString(),
      device_headers: {
        app_client: "consumer_web",
        platform: "mobile_web",
        "user-agent": snapshot.userAgent || "",
      },
    },
  };
}

async function runBootstrapFlow(context, page, timeout) {
  const deadline = newDeadline(timeout);
  await page.goto("https://blinkit.com/", { waitUntil: "domcontentloaded", timeout });
  let snapshot = await readSessionSnapshot(context, page);
  if (snapshot.accessToken) {
    return buildBootstrapResult(snapshot);
  }
  debugLog("bootstrap awaiting login/session in persistent profile");
  while (Date.now() < deadline) {
    await page.waitForTimeout(2000);
    snapshot = await readSessionSnapshot(context, page);
    if (snapshot.accessToken) {
      return buildBootstrapResult(snapshot);
    }
  }
  return {
    ok: false,
    error: "access token not found in browser profile; complete Blinkit login and retry bootstrap",
  };
}

async function runOneShot(args) {
  const timeout = timeoutMs();
  const chromium = await loadChromium();
  const { context, page, headless } = await launchBrowserSession(chromium, timeout);
  debugLog(`search start mode=oneshot headless=${headless} timeout_ms=${timeout}`);
  try {
    const result = await runSearch(context, page, args.query, args.lat, args.lng, timeout);
    print(result);
  } finally {
    await context.close();
  }
}

async function runBootstrapOneShot() {
  const timeout = bootstrapTimeoutMs();
  const chromium = await loadChromium();
  const { context, page, headless } = await launchBrowserSession(chromium, timeout);
  debugLog(`bootstrap start mode=oneshot headless=${headless} timeout_ms=${timeout}`);
  try {
    const result = await runBootstrapFlow(context, page, timeout);
    print(result);
  } finally {
    await context.close();
  }
}

async function runWorker(args) {
  const timeout = timeoutMs();
  const chromium = await loadChromium();
  const { context, page, headless } = await launchBrowserSession(chromium, timeout);
  const bind = workerBind(args);
  const port = workerPort(args);
  const state = { queue: Promise.resolve() };

  const server = http.createServer(async (req, res) => {
    if (req.method === "GET" && req.url === "/health") {
      writeJSON(res, 200, { status: "ok" });
      return;
    }
    if (req.method !== "POST" || (req.url !== "/search" && req.url !== "/bootstrap")) {
      writeJSON(res, 404, { ok: false, error: "not found" });
      return;
    }

    let body = {};
    try {
      body = await readJSONBody(req);
    } catch (err) {
      writeJSON(res, 400, { ok: false, error: `invalid JSON body: ${cleanError(err)}` });
      return;
    }

    try {
      const result = await enqueueTask(state, async () => {
        if (req.url === "/search") {
          const query = String(body.query || "").trim();
          if (!query) {
            throw new Error("query is required");
          }
          const lat = toOptionalNumber(body.lat);
          const lng = toOptionalNumber(body.lng);
          const searchTimeout = searchTimeoutMs(body);
          debugLog(`search start mode=worker headless=${headless} timeout_ms=${searchTimeout} query=${query}`);
          return runSearch(context, page, query, lat, lng, searchTimeout);
        }
        const bootstrapTimeout = bootstrapTimeoutMs(body);
        debugLog(`bootstrap start mode=worker headless=${headless} timeout_ms=${bootstrapTimeout}`);
        return runBootstrapFlow(context, page, bootstrapTimeout);
      });
      writeJSON(res, 200, result);
    } catch (err) {
      writeJSON(res, 200, { ok: false, error: cleanError(err) });
    }
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, bind, resolve);
  });
  debugLog(`worker listening on http://${bind}:${port}`);

  const shutdown = async (signal) => {
    debugLog(`worker shutdown signal=${signal}`);
    server.close(() => {
      // no-op
    });
    try {
      await context.close();
    } catch {
      // ignore
    }
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);

  await new Promise(() => {});
}

try {
  const args = parseArgs(process.argv.slice(2));
  if (args.worker) {
    await runWorker(args);
  } else if (args.bootstrap) {
    await runBootstrapOneShot();
  } else {
    await runOneShot(args);
  }
} catch (err) {
  print({ ok: false, error: cleanError(err) });
  process.exitCode = 1;
}
