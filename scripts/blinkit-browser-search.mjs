#!/usr/bin/env node
// Blinkit browser search helper — puppeteer-extra + stealth + fingerprint injection.
// CLI: --query <q> [--lat <n>] [--lng <n>] | --worker [--worker-port <n>] | --bootstrap
// Env: see SETUP.md for full variable list.

import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import process from "node:process";
import { createRequire } from "node:module";
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const require = createRequire(import.meta.url);
const execFileAsync = promisify(execFile);

// ─── constants ────────────────────────────────────────────────────────────────

const DEFAULT_TIMEOUT_MS = 45000;
const DEFAULT_BOOTSTRAP_TIMEOUT_MS = 300000;
const DEFAULT_WORKER_PORT = 42199;
const DEFAULT_WORKER_BIND = "127.0.0.1";
const SEARCH_HOST_SUFFIX = "blinkit.com";
const SEARCH_PATH = "/v1/layout/search";
const CHALLENGE_MARKERS = [
  "just a moment",
  "attention required",
  "verify you are human",
  "cloudflare ray id",
  "cf-challenge",
  "captcha",
  "access denied",
];

// Realistic iPhone 14 Pro viewport for Blinkit mobile-web.
const MOBILE_VIEWPORT = { width: 390, height: 844, deviceScaleFactor: 3 };

// Default iOS Chrome UA — overridden by injected fingerprint when available.
const DEFAULT_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) " +
  "AppleWebKit/605.1.15 (KHTML, like Gecko) " +
  "CriOS/120.0.6099.119 Mobile/15E148 Safari/604.1";

// ─── CLI args ─────────────────────────────────────────────────────────────────

function parseArgs(argv) {
  const args = { query: "", lat: null, lng: null, worker: false, bootstrap: false, workerPort: null, bind: null };
  for (let i = 0; i < argv.length; i += 1) {
    const key = argv[i];
    const val = argv[i + 1];
    if (key === "--worker") { args.worker = true; continue; }
    if (key === "--bootstrap") { args.bootstrap = true; continue; }
    if (key === "--worker-port" && val) { args.workerPort = Number.parseInt(val, 10); i += 1; continue; }
    if (key === "--bind" && val) { args.bind = val; i += 1; continue; }
    if (key === "--query" && val) { args.query = val; i += 1; continue; }
    if (key === "--lat" && val) { args.lat = Number.parseFloat(val); i += 1; continue; }
    if (key === "--lng" && val) { args.lng = Number.parseFloat(val); i += 1; }
  }
  if (!args.worker && !args.bootstrap && !args.query.trim()) {
    throw new Error("missing required --query argument");
  }
  return args;
}

// ─── env helpers ──────────────────────────────────────────────────────────────

function parseBool(raw, fallback) {
  if (raw === undefined) return fallback;
  const v = String(raw).trim().toLowerCase();
  if (v === "1" || v === "true" || v === "yes") return true;
  if (v === "0" || v === "false" || v === "no") return false;
  return fallback;
}

function timeoutMs() {
  const raw = process.env.BLINKIT_BROWSER_TIMEOUT_MS;
  if (!raw) return DEFAULT_TIMEOUT_MS;
  const p = Number.parseInt(raw, 10);
  return Number.isNaN(p) || p <= 0 ? DEFAULT_TIMEOUT_MS : p;
}

function bootstrapTimeoutMs(body = {}) {
  const bv = Number.parseInt(body.timeout_seconds, 10);
  if (Number.isFinite(bv) && bv > 0) return bv * 1000;
  const raw = process.env.BLINKIT_BROWSER_BOOTSTRAP_TIMEOUT_MS;
  if (!raw) return DEFAULT_BOOTSTRAP_TIMEOUT_MS;
  const p = Number.parseInt(raw, 10);
  return Number.isNaN(p) || p <= 0 ? DEFAULT_BOOTSTRAP_TIMEOUT_MS : p;
}

function searchTimeoutMs(body = {}) {
  const bv = Number.parseInt(body.timeout_seconds, 10);
  if (Number.isFinite(bv) && bv > 0) return bv * 1000;
  return timeoutMs();
}

function workerPort(args) {
  if (Number.isFinite(args.workerPort) && args.workerPort > 0) return args.workerPort;
  const raw = process.env.BLINKIT_BROWSER_WORKER_PORT;
  if (!raw) return DEFAULT_WORKER_PORT;
  const p = Number.parseInt(raw, 10);
  return Number.isNaN(p) || p <= 0 ? DEFAULT_WORKER_PORT : p;
}

function workerBind(args) {
  const v = args.bind || process.env.BLINKIT_BROWSER_WORKER_BIND;
  return (v && String(v).trim()) || DEFAULT_WORKER_BIND;
}

function debugEnabled() { return parseBool(process.env.BLINKIT_BROWSER_DEBUG, false); }

function debugLog(msg) {
  if (!debugEnabled()) return;
  process.stderr.write(`[blinkit-browser] ${msg}\n`);
}

function newDeadline(timeout) { return Date.now() + timeout; }
function remainingTimeout(deadline) { return Math.max(1, deadline - Date.now()); }

// ─── puppeteer-extra + stealth loader ─────────────────────────────────────────

function loadPuppeteerExtra() {
  let puppeteer;
  try {
    puppeteer = require("puppeteer-extra");
  } catch {
    throw new Error(
      "puppeteer-extra is not installed.\n" +
      "Run: npm install   (package.json lists all required deps)"
    );
  }
  try {
    const StealthPlugin = require("puppeteer-extra-plugin-stealth");
    puppeteer.use(StealthPlugin());
    debugLog("stealth plugin loaded");
  } catch {
    debugLog("warning: puppeteer-extra-plugin-stealth not found — running without stealth");
  }
  return puppeteer;
}

// ─── fingerprint generation & injection ──────────────────────────────────────

function generateFingerprint() {
  try {
    const { FingerprintGenerator } = require("fingerprint-generator");
    const gen = new FingerprintGenerator({
      browsers: ["chrome"],
      devices: ["mobile"],
      operatingSystems: ["ios"],
      locales: ["en-IN"],
    });
    const fp = gen.getFingerprint();
    debugLog("fingerprint generated");
    return fp;
  } catch {
    debugLog("warning: @apify/fingerprint-generator not found — skipping fingerprint");
    return null;
  }
}

async function injectFingerprint(page, fingerprint) {
  if (!fingerprint) return;
  try {
    const { FingerprintInjector } = require("fingerprint-injector");
    const injector = new FingerprintInjector();
    await injector.injectFingerprintToPuppeteer(page, fingerprint);
    debugLog("fingerprint injected");
  } catch {
    debugLog("warning: fingerprint-injector not found — skipping injection");
  }
}

// ─── Chromium executable auto-detection ───────────────────────────────────────

async function findChromiumExecutable() {
  const envPath = String(process.env.BLINKIT_BROWSER_EXECUTABLE_PATH || "").trim();
  if (envPath && fs.existsSync(envPath)) return envPath;

  // Reuse Playwright's downloaded Chromium if available.
  try {
    const playwrightCore = await import("playwright-core");
    const execPath = playwrightCore.chromium.executablePath();
    if (execPath && fs.existsSync(execPath)) {
      debugLog(`using playwright chromium: ${execPath}`);
      return execPath;
    }
  } catch { /* not installed */ }

  // System Chrome / Chromium.
  const candidates = [
    "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
    "/usr/local/bin/chromium",
  ];
  for (const c of candidates) {
    if (fs.existsSync(c)) { debugLog(`using system chrome: ${c}`); return c; }
  }
  return null;
}

// ─── browser session ──────────────────────────────────────────────────────────

async function launchBrowserSession(puppeteer, fingerprint, timeout) {
  const profileDir = path.resolve(
    process.env.BLINKIT_BROWSER_PROFILE_DIR || ".data/blinkit-browser-profile"
  );
  fs.mkdirSync(profileDir, { recursive: true });

  const headless = parseBool(process.env.BLINKIT_BROWSER_HEADLESS, true);
  const executablePath = await findChromiumExecutable();

  const launchArgs = [
    "--no-sandbox",
    "--disable-setuid-sandbox",
    "--disable-blink-features=AutomationControlled",
    "--disable-infobars",
    "--disable-dev-shm-usage",
    "--disable-accelerated-2d-canvas",
    "--disable-gpu",
    "--disable-software-rasterizer",
    "--no-zygote",
    "--no-first-run",
    "--disable-extensions",
    "--disable-session-crashed-bubble",
    "--disable-restore-session-state",
    "--no-default-browser-check",
    `--window-size=${MOBILE_VIEWPORT.width},${MOBILE_VIEWPORT.height}`,
  ];

  const proxyServer = String(process.env.BLINKIT_BROWSER_PROXY_SERVER || "").trim();
  if (proxyServer) launchArgs.push(`--proxy-server=${proxyServer}`);

  const launchOptions = {
    headless: headless ? "new" : false,
    userDataDir: profileDir,
    args: launchArgs,
    defaultViewport: MOBILE_VIEWPORT,
    ignoreHTTPSErrors: true,
  };
  if (executablePath) launchOptions.executablePath = executablePath;

  const browser = await puppeteer.launch(launchOptions);

  // Close all pages restored from previous session — they may have dead frames.
  const existingPages = await browser.pages();
  const page = await browser.newPage();
  for (const p of existingPages) await p.close().catch(() => {});

  // Proxy auth (only when proxy is configured).
  const proxyUser = String(process.env.BLINKIT_BROWSER_PROXY_USERNAME || "").trim();
  const proxyPass = String(process.env.BLINKIT_BROWSER_PROXY_PASSWORD || "").trim();
  if (proxyServer && proxyUser) {
    await page.authenticate({ username: proxyUser, password: proxyPass });
  }

  // User-agent: prefer fingerprint UA, else use iOS Chrome default.
  const ua = fingerprint?.navigator?.userAgent || DEFAULT_UA;
  await page.setUserAgent(ua);
  await page.setViewport(MOBILE_VIEWPORT);

  // Inject realistic fingerprint (canvas, WebGL, screen, fonts…).
  await injectFingerprint(page, fingerprint);

  // Inject persisted iPhone cookies if configured.
  await injectSavedCookies(page);

  page.setDefaultNavigationTimeout(timeout);
  page.setDefaultTimeout(timeout);

  return { browser, page, headless };
}

// ─── cookie injection ─────────────────────────────────────────────────────────

async function injectSavedCookies(page) {
  const cookiesFile = String(process.env.BLINKIT_BROWSER_COOKIES_FILE || "").trim();
  if (!cookiesFile) return;
  try {
    const raw = fs.readFileSync(cookiesFile, "utf8");
    const cookies = JSON.parse(raw);
    if (Array.isArray(cookies) && cookies.length > 0) {
      await page.setCookie(...cookies);
      debugLog(`injected ${cookies.length} cookies from ${cookiesFile}`);
    }
  } catch (err) {
    debugLog(`warning: could not inject cookies: ${cleanError(err)}`);
  }
}

// ─── geolocation (puppeteer CDP) ──────────────────────────────────────────────

async function applyGeolocation(browser, page, lat, lng) {
  if (lat === null || lng === null) return;
  try {
    await browser.defaultBrowserContext().overridePermissions("https://blinkit.com", ["geolocation"]);
    await page.setGeolocation({ latitude: lat, longitude: lng, accuracy: 50 });
  } catch (err) {
    debugLog(`geolocation override failed: ${cleanError(err)}`);
  }
}

// ─── curl-impersonate direct API call (optional, bypasses TLS fingerprint) ────

async function trySearchWithCurlImpersonate(query, lat, lng, pageCookies, timeout) {
  const binary = String(process.env.BLINKIT_CURL_IMPERSONATE || "").trim();
  if (!binary) return null;

  const url =
    `https://blinkit.com${SEARCH_PATH}` +
    `?q=${encodeURIComponent(query)}&search_type=type_to_search`;

  const reqHeaders = {
    accept: "application/json,text/plain,*/*",
    app_client: "consumer_web",
    platform: "mobile_web",
    web_app_version: "1008010016",
    rn_bundle_version: "1009003012",
  };
  if (lat !== null) { reqHeaders.lat = String(lat); reqHeaders.cur_lat = String(lat); }
  if (lng !== null) { reqHeaders.lon = String(lng); reqHeaders.cur_lon = String(lng); }

  const args = [url, "--silent", "--max-time", String(Math.ceil(timeout / 1000)), "--compressed"];
  for (const [k, v] of Object.entries(reqHeaders)) args.push("-H", `${k}: ${v}`);

  if (pageCookies.length > 0) {
    const cookieStr = pageCookies.map((c) => `${c.name}=${c.value}`).join("; ");
    args.push("-H", `Cookie: ${cookieStr}`);
  }

  try {
    const { stdout } = await execFileAsync(binary, args, { timeout: timeout + 5000 });
    const body = JSON.parse(stdout.trim());
    debugLog(`curl-impersonate search ok status=200 query=${query}`);
    return { ok: true, url, status: 200, raw: body };
  } catch (err) {
    debugLog(`curl-impersonate failed: ${cleanError(err)}`);
    return null;
  }
}

// ─── response interception ────────────────────────────────────────────────────

function isSearchResponse(response) {
  try {
    const url = new URL(response.url());
    if (!url.hostname.endsWith(SEARCH_HOST_SUFFIX)) return false;
    if (url.pathname !== SEARCH_PATH) return false;
    const method = response.request().method().toUpperCase();
    return method === "GET" || method === "POST";
  } catch {
    return false;
  }
}

// Returns a Promise that resolves with the first matching search response,
// or null on timeout. Registers a one-shot response listener on `page`.
function makeSearchResponsePromise(page, timeout) {
  return new Promise((resolve) => {
    let settled = false;
    const done = (value) => {
      if (settled) return;
      settled = true;
      page.off("response", handler);
      clearTimeout(timer);
      resolve(value);
    };
    const timer = setTimeout(() => done(null), timeout);
    const handler = (response) => { if (isSearchResponse(response)) done(response); };
    page.on("response", handler);
  });
}

// ─── page navigation helpers ──────────────────────────────────────────────────

async function waitForSearchFromUrl(page, targetUrl, timeout) {
  const pending = makeSearchResponsePromise(page, timeout);
  await page.goto(targetUrl, { waitUntil: "domcontentloaded", timeout });
  return pending;
}

async function waitForSearchFromHome(page, query, timeout) {
  const pending = makeSearchResponsePromise(page, timeout);
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
    const el = await page.$(selector);
    if (el) { input = el; break; }
  }
  if (!input) return pending;

  await input.click();
  await page.keyboard.type(query, { delay: 50 });
  await page.keyboard.press("Enter");
  return pending;
}

// ─── in-page fetch fallback ───────────────────────────────────────────────────

async function fetchSearchViaPage(page, query, lat, lng, timeout) {
  const endpoint = `${SEARCH_PATH}?q=${encodeURIComponent(query)}&search_type=type_to_search`;
  const withCoords = toOptionalNumber(lat) !== null && toOptionalNumber(lng) !== null;

  const raw = await page.evaluate(
    async ({ endpoint, timeout, lat, lng, withCoords }) => {
      const decodeMaybe = (v) => { try { return decodeURIComponent(v); } catch { return v; } };
      const pick = (...vals) => { for (const v of vals) if (typeof v === "string" && v.trim()) return v.trim(); return ""; };
      const readCookieMap = () => {
        const m = {};
        for (const entry of (document.cookie || "").split(";")) {
          const p = entry.trim(); if (!p) continue;
          const idx = p.indexOf("="); if (idx <= 0) continue;
          m[p.slice(0, idx).trim()] = p.slice(idx + 1).trim();
        }
        return m;
      };
      const readLS = (...keys) => {
        for (const k of keys) { const v = window.localStorage.getItem(k); if (typeof v === "string" && v.trim()) return v.trim(); }
        return "";
      };
      const cookies = readCookieMap();
      const accessToken = pick(readLS("gr_1_accessToken", "access_token", "accessToken"), decodeMaybe(cookies.gr_1_accessToken));
      const authKey = pick(readLS("auth_key", "gr_1_authKey"), decodeMaybe(cookies.auth_key));
      const deviceID = pick(readLS("gr_1_deviceId", "device_id"), decodeMaybe(cookies.gr_1_deviceId));
      const sessionUUID = pick(readLS("session_uuid"), decodeMaybe(cookies.session_uuid));

      const buildHeaders = (method) => {
        const h = { accept: "application/json,text/plain,*/*", "content-type": "application/json", app_client: "consumer_web", platform: "mobile_web", web_app_version: "1008010016", rn_bundle_version: "1009003012" };
        if (method === "GET") delete h["content-type"];
        if (accessToken) h.access_token = accessToken;
        if (authKey) h.auth_key = authKey;
        if (deviceID) h.device_id = deviceID;
        if (sessionUUID) h.session_uuid = sessionUUID;
        if (withCoords) { h.lat = String(lat); h.lon = String(lng); h.cur_lat = String(lat); h.cur_lon = String(lng); }
        return h;
      };
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeout);
      const doFetch = async (method) => {
        const resp = await fetch(endpoint, { method, credentials: "include", signal: controller.signal, headers: buildHeaders(method), body: method === "POST" ? "{}" : undefined });
        const text = await resp.text();
        return { ok: resp.ok, status: resp.status, url: resp.url, text, method };
      };
      try {
        const first = await doFetch("GET");
        if (first.ok || first.status !== 403) return first;
        return doFetch("POST");
      } catch (err) {
        return { ok: false, status: 0, url: endpoint, text: "", error: err?.message ? String(err.message) : String(err) };
      } finally { clearTimeout(timer); }
    },
    { endpoint, timeout, lat, lng, withCoords }
  );

  if (raw.error) throw new Error(`in-page fetch failed: ${raw.error}`);
  if (!raw.ok) {
    if (raw.status === 403 && textContainsChallenge(raw.text)) {
      throw new Error("human_verification_required: anti-bot challenge page returned 403");
    }
    const snippet = typeof raw.text === "string" ? raw.text.replace(/\s+/g, " ").trim().slice(0, 180) : "";
    throw new Error(snippet ? `in-page fetch status ${raw.status} (${raw.url}): ${snippet}` : `in-page fetch status ${raw.status} (${raw.url})`);
  }
  try {
    return { url: raw.url, status: raw.status, body: JSON.parse(raw.text) };
  } catch {
    throw new Error("in-page fetch returned non-JSON response");
  }
}

// ─── helpers ──────────────────────────────────────────────────────────────────

function toOptionalNumber(value) {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") { const p = Number.parseFloat(value); if (Number.isFinite(p)) return p; }
  return null;
}

function print(payload) { process.stdout.write(`${JSON.stringify(payload)}\n`); }

function cleanError(err) {
  if (!err) return "unknown error";
  return (typeof err === "string" ? err : err.message || String(err)).replace(/\s+/g, " ").trim();
}

function textContainsChallenge(text) {
  if (typeof text !== "string" || !text.trim()) return false;
  const lower = text.toLowerCase();
  return CHALLENGE_MARKERS.some((m) => lower.includes(m));
}

function writeJSON(res, status, payload) {
  res.statusCode = status;
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify(payload));
}

async function readJSONBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const text = Buffer.concat(chunks).toString("utf8");
  if (!text.trim()) return {};
  return JSON.parse(text);
}

function enqueueTask(state, task) {
  const next = state.queue.then(task, task);
  state.queue = next.then(() => undefined, () => undefined);
  return next;
}

function splitSearchBudget(totalMs) {
  const timeout = Number.isFinite(totalMs) && totalMs > 0 ? totalMs : DEFAULT_TIMEOUT_MS;
  const min = 1000;
  let directMs = Math.floor(timeout * 0.5);
  let homeMs = Math.floor(timeout * 0.3);
  let fetchMs = timeout - directMs - homeMs;
  if (directMs < min) directMs = min;
  if (homeMs < min) homeMs = min;
  if (fetchMs < min) {
    const deficit = min - fetchMs;
    const cutHome = Math.min(deficit, Math.max(0, homeMs - min));
    homeMs -= cutHome;
    const remaining = deficit - cutHome;
    const cutDirect = Math.min(remaining, Math.max(0, directMs - min));
    directMs -= cutDirect;
    fetchMs = timeout - directMs - homeMs;
    if (fetchMs < min) fetchMs = min;
  }
  return { directMs, homeMs, fetchMs };
}

// ─── challenge detection ──────────────────────────────────────────────────────

async function pageLooksLikeChallenge(page) {
  try {
    const snapshot = await page.evaluate(() => {
      const title = document.title || "";
      const body = (document.body && document.body.innerText) || "";
      return `${title}\n${body.slice(0, 4000)}`;
    });
    return textContainsChallenge(snapshot);
  } catch {
    return false;
  }
}

// ─── DOM product extraction ───────────────────────────────────────────────────

async function extractProductsFromDOM(page, query) {
  return page.evaluate(({ query }) => {
    const normalize = (v) => (typeof v === "string" ? v.trim() : "");
    const toNumber = (v) => {
      if (typeof v === "number" && Number.isFinite(v)) return v;
      if (typeof v === "string") { const c = v.replace(/[^\d.]/g, ""); if (c) { const p = Number.parseFloat(c); if (Number.isFinite(p)) return p; } }
      return null;
    };
    const toBool = (v, fb = true) => {
      if (typeof v === "boolean") return v;
      if (typeof v === "number") return v > 0;
      if (typeof v === "string") {
        const l = v.toLowerCase();
        if (["true", "yes", "in_stock", "available"].includes(l)) return true;
        if (["false", "no", "out_of_stock", "unavailable"].includes(l)) return false;
      }
      return fb;
    };
    const mapStr = (o, keys) => { for (const k of keys) { const v = normalize(o?.[k]); if (v) return v; } return ""; };
    const mapNum = (o, keys) => { for (const k of keys) { const v = toNumber(o?.[k]); if (v !== null) return v; } return null; };
    const seen = new Set();
    const products = [];
    const append = (c) => {
      const id = normalize(c.id || c.product_id);
      const name = normalize(c.name || c.title || c.product_name);
      const price = toNumber(c.price ?? c.selling_price ?? c.final_price);
      if (!name || price === null) return;
      const key = id || `${name.toLowerCase()}::${price}`;
      if (seen.has(key)) return;
      seen.add(key);
      const mrp = toNumber(c.mrp ?? c.original_price) ?? price;
      products.push({ id: id || key, name, brand: normalize(c.brand), price, mrp, unit: normalize(c.unit || c.quantity), store_id: normalize(c.store_id || c.storeId), in_stock: toBool(c.in_stock ?? c.is_available ?? c.available ?? c.stock, true) });
    };
    const walk = (v) => {
      if (!v) return;
      if (Array.isArray(v)) { v.forEach(walk); return; }
      if (typeof v !== "object") return;
      append({ id: mapStr(v, ["id", "product_id", "variant_id", "sku", "entity_id"]), name: mapStr(v, ["name", "title", "product_name"]), brand: mapStr(v, ["brand", "brand_name"]), price: mapNum(v, ["price", "selling_price", "final_price", "sp"]), mrp: mapNum(v, ["mrp", "original_price", "list_price"]), unit: mapStr(v, ["unit", "quantity", "pack_size"]), in_stock: v.in_stock ?? v.available ?? v.is_available ?? v.stock, store_id: mapStr(v, ["store_id", "storeId", "store"]) });
      Object.values(v).forEach(walk);
    };
    for (const script of Array.from(document.querySelectorAll("script"))) {
      const text = script.textContent || "";
      if (!text || text.length > 1_500_000) continue;
      const t = text.trim();
      if (!t.startsWith("{") && !t.startsWith("[")) continue;
      try { walk(JSON.parse(t)); } catch { /* ignore */ }
    }
    try { if (window.__NEXT_DATA__) walk(window.__NEXT_DATA__); } catch {}
    try { if (window.__INITIAL_STATE__) walk(window.__INITIAL_STATE__); } catch {}
    try { if (window.__APOLLO_STATE__) walk(window.__APOLLO_STATE__); } catch {}
    if (products.length === 0) {
      const cards = Array.from(document.querySelectorAll('[data-testid*="product"],[class*="product"],[class*="Product"]')).slice(0, 80);
      for (const card of cards) {
        const text = (card.textContent || "").replace(/\s+/g, " ").trim(); if (!text) continue;
        const priceMatch = text.match(/(?:₹|Rs\.?)\s*([0-9]+(?:\.[0-9]+)?)/i); if (!priceMatch) continue;
        const nameLine = text.split(/₹|Rs\.?/i)[0].replace(/\s{2,}/g, " ").trim(); if (!nameLine) continue;
        append({ id: "", name: nameLine, price: priceMatch[1], mrp: priceMatch[1], in_stock: !/out of stock/i.test(text) });
      }
    }
    const q = normalize(query).toLowerCase();
    return products.map((p) => {
      const n = normalize(p.name).toLowerCase();
      const score = q && n.includes(q) ? 2 : q && q.split(/\s+/).some((t) => n.includes(t)) ? 1 : 0;
      return { score, product: p };
    }).sort((a, b) => b.score - a.score || a.product.price - b.product.price).map((x) => x.product).slice(0, 40);
  }, { query });
}

// ─── search orchestration ─────────────────────────────────────────────────────

async function runSearch(browser, page, query, lat, lng, timeout) {
  const budget = splitSearchBudget(timeout);
  await applyGeolocation(browser, page, lat, lng);

  // Prefer curl-impersonate when configured — correct TLS fingerprint, no browser overhead.
  const pageCookies = await page.cookies("https://blinkit.com").catch(() => []);
  const curlResult = await trySearchWithCurlImpersonate(query, lat, lng, pageCookies, budget.directMs);
  if (curlResult) return curlResult;

  // Browser path: navigate to search URL, intercept network response.
  const searchURL = `https://blinkit.com/s/?q=${encodeURIComponent(query)}`;
  let response = await waitForSearchFromUrl(page, searchURL, budget.directMs);

  if (!response && (await pageLooksLikeChallenge(page))) {
    return { ok: false, error: "human_verification_required: blinkit anti-bot challenge detected in browser session" };
  }
  if (!response) {
    debugLog("direct URL search response not observed, trying home-page fallback");
    response = await waitForSearchFromHome(page, query, budget.homeMs);
  }

  if (response) {
    debugLog(`search response captured status=${response.status()}`);
    const body = await response.json();
    return { ok: true, url: response.url(), status: response.status(), raw: body };
  }

  if (await pageLooksLikeChallenge(page)) {
    return { ok: false, error: "human_verification_required: blinkit anti-bot challenge detected in browser session" };
  }

  const domProducts = await extractProductsFromDOM(page, query);
  if (domProducts.length > 0) {
    debugLog(`dom extraction yielded products=${domProducts.length}`);
    return { ok: true, url: page.url(), status: 200, raw: { products: domProducts, source: "dom" } };
  }

  debugLog("network interception miss; using in-page fetch fallback");
  const fallback = await fetchSearchViaPage(page, query, lat, lng, budget.fetchMs);
  return { ok: true, url: fallback.url, status: fallback.status, raw: fallback.body };
}

// ─── session snapshot ─────────────────────────────────────────────────────────

async function sessionStatusSnapshot(page) {
  const fallback = { title: "", body: "", accessTokenPresent: false, authKeyPresent: false };
  if (!page || page.isClosed()) return fallback;
  return page.evaluate(() => {
    const safeGet = (...keys) => { for (const k of keys) { try { const v = window.localStorage.getItem(k); if (typeof v === "string" && v.trim()) return v.trim(); } catch {} } return ""; };
    const title = document.title || "";
    const body = (document.body && document.body.innerText) || "";
    const access = safeGet("gr_1_accessToken", "access_token", "accessToken");
    const authKey = safeGet("auth_key", "gr_1_authKey");
    return { title, body: body.slice(0, 4000), accessTokenPresent: Boolean(access), authKeyPresent: Boolean(authKey) };
  }).catch(() => fallback);
}

function decodeMaybe(v) { if (typeof v !== "string") return ""; try { return decodeURIComponent(v); } catch { return v; } }
function firstNonEmpty(...vals) { for (const v of vals) if (typeof v === "string" && v.trim()) return v.trim(); return ""; }

function findTokenByKey(store, cookies, keys) {
  const norm = new Set(keys.map((k) => k.toLowerCase()));
  for (const [k, v] of Object.entries(store)) if (norm.has(k.toLowerCase()) && String(v).trim()) return String(v).trim();
  for (const c of cookies) if (norm.has(String(c.name).toLowerCase())) { const v = decodeMaybe(c.value); if (v.trim()) return v.trim(); }
  return "";
}

function findAccessTokenFallback(store, cookies) {
  const candidates = [...Object.values(store).filter((v) => typeof v === "string"), ...cookies.map((c) => decodeMaybe(c.value))];
  return candidates.find((v) => v.startsWith("v2::")) || "";
}

async function readSessionSnapshot(page) {
  const storage = await page.evaluate(() => {
    const out = {};
    const len = (() => { try { return window.localStorage.length; } catch { return 0; } })();
    for (let i = 0; i < len; i += 1) {
      const k = (() => { try { return window.localStorage.key(i); } catch { return ""; } })();
      if (!k) continue;
      out[k] = (() => { try { return window.localStorage.getItem(k); } catch { return ""; } })() || "";
    }
    return { localStorage: out, userAgent: navigator.userAgent || "" };
  });
  const cookies = await page.cookies("https://blinkit.com");
  const ls = storage.localStorage || {};
  const accessToken = firstNonEmpty(
    findTokenByKey(ls, cookies, ["gr_1_accessToken", "access_token", "accessToken"]),
    findAccessTokenFallback(ls, cookies)
  );
  const refreshToken = firstNonEmpty(
    findTokenByKey(ls, cookies, ["auth_key", "refresh_token", "refreshToken", "gr_1_authKey", "gr_1_refreshToken"]),
    accessToken
  );
  return { accessToken, refreshToken, userAgent: storage.userAgent || "" };
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
      device_headers: { app_client: "consumer_web", platform: "mobile_web", "user-agent": snapshot.userAgent || "" },
    },
  };
}

// ─── bootstrap flow ───────────────────────────────────────────────────────────

async function runBootstrapFlow(page, timeout) {
  const challengeError = "human_verification_required: blinkit anti-bot challenge detected in browser session";
  const deadline = newDeadline(timeout);
  await page.goto("https://blinkit.com/", { waitUntil: "domcontentloaded", timeout });
  if (await pageLooksLikeChallenge(page)) return { ok: false, error: challengeError };
  let snapshot = await readSessionSnapshot(page);
  if (snapshot.accessToken) return buildBootstrapResult(snapshot);
  debugLog("bootstrap awaiting login/session in persistent profile");
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 2000));
    if (await pageLooksLikeChallenge(page)) return { ok: false, error: challengeError };
    snapshot = await readSessionSnapshot(page);
    if (snapshot.accessToken) return buildBootstrapResult(snapshot);
  }
  const status = await sessionStatusSnapshot(page);
  if (textContainsChallenge(`${status.title}\n${status.body}`)) return { ok: false, error: challengeError };
  const titlePart = status.title ? `; page_title="${status.title}"` : "";
  return { ok: false, error: `access token not found in browser profile; complete Blinkit login and retry bootstrap${titlePart}` };
}

// ─── runners ──────────────────────────────────────────────────────────────────

async function runOneShot(args) {
  const timeout = timeoutMs();
  const puppeteer = loadPuppeteerExtra();
  const fingerprint = generateFingerprint();
  const { browser, page, headless } = await launchBrowserSession(puppeteer, fingerprint, timeout);
  debugLog(`search start mode=oneshot headless=${headless} timeout_ms=${timeout}`);
  try {
    const result = await runSearch(browser, page, args.query, args.lat, args.lng, timeout);
    print(result);
  } finally {
    await browser.close().catch(() => {});
  }
}

async function runBootstrapOneShot() {
  const timeout = bootstrapTimeoutMs();
  const puppeteer = loadPuppeteerExtra();
  const fingerprint = generateFingerprint();
  const { browser, page, headless } = await launchBrowserSession(puppeteer, fingerprint, timeout);
  debugLog(`bootstrap start mode=oneshot headless=${headless} timeout_ms=${timeout}`);
  try {
    const result = await runBootstrapFlow(page, timeout);
    print(result);
  } finally {
    await browser.close().catch(() => {});
  }
}

async function runWorker(args) {
  const timeout = timeoutMs();
  const puppeteer = loadPuppeteerExtra();
  const fingerprint = generateFingerprint();
  const { browser, page, headless } = await launchBrowserSession(puppeteer, fingerprint, timeout);
  const bind = workerBind(args);
  const port = workerPort(args);
  const state = { queue: Promise.resolve(), page };

  // Recreate page on crash so the worker stays alive.
  page.on("crash", async () => {
    debugLog("page crashed — recreating");
    try {
      state.page = await browser.newPage();
      await state.page.goto("about:blank", { waitUntil: "load", timeout: 10000 }).catch(() => {});
    } catch (err) {
      debugLog(`page recreate failed: ${cleanError(err)}`);
    }
  });

  const server = http.createServer(async (req, res) => {
    if (req.method === "GET" && req.url === "/health") {
      writeJSON(res, 200, { status: "ok" });
      return;
    }
    if (req.method === "GET" && req.url === "/status") {
      try {
        const status = await enqueueTask(state, async () => {
          const pg = state.page;
          const snapshot = await sessionStatusSnapshot(pg);
          return { ok: true, page_url: pg.url(), title: snapshot.title, access_token_present: snapshot.accessTokenPresent, auth_key_present: snapshot.authKeyPresent, challenge_detected: textContainsChallenge(`${snapshot.title}\n${snapshot.body}`) };
        });
        writeJSON(res, 200, status);
      } catch (err) {
        writeJSON(res, 200, { ok: false, error: cleanError(err) });
      }
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
          if (!query) throw new Error("query is required");
          const lat = toOptionalNumber(body.lat);
          const lng = toOptionalNumber(body.lng);
          const st = searchTimeoutMs(body);
          debugLog(`search start mode=worker headless=${headless} timeout_ms=${st} query=${query}`);
          return runSearch(browser, state.page, query, lat, lng, st);
        }
        const bt = bootstrapTimeoutMs(body);
        debugLog(`bootstrap start mode=worker headless=${headless} timeout_ms=${bt}`);
        return runBootstrapFlow(state.page, bt);
      });
      writeJSON(res, 200, result);
    } catch (err) {
      writeJSON(res, 200, { ok: false, error: cleanError(err) });
    }
  });

  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(port, bind, resolve); });
  debugLog(`worker listening on http://${bind}:${port}`);

  const shutdown = async (signal) => {
    debugLog(`worker shutdown signal=${signal}`);
    server.close(() => {});
    try { await browser.close(); } catch {}
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
  await new Promise(() => {});
}

// ─── entry point ──────────────────────────────────────────────────────────────

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
