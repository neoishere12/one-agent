#!/usr/bin/env node
// Blinkit browser search helper — playwright-extra + stealth + fingerprint injection.
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
function debugLog(msg) { if (!debugEnabled()) return; process.stderr.write(`[blinkit-browser] ${msg}\n`); }
function newDeadline(timeout) { return Date.now() + timeout; }

// ─── playwright-extra + stealth loader ────────────────────────────────────────

function loadChromium() {
  let chromium;
  try {
    const playwrightExtra = require("playwright-extra");
    chromium = playwrightExtra.chromium;
  } catch {
    throw new Error(
      "playwright-extra is not installed.\nRun: npm install"
    );
  }
  try {
    const StealthPlugin = require("puppeteer-extra-plugin-stealth");
    chromium.use(StealthPlugin());
    debugLog("stealth plugin loaded");
  } catch {
    debugLog("warning: stealth plugin not found — running without stealth");
  }
  return chromium;
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
    debugLog("warning: fingerprint-generator not found — skipping");
    return null;
  }
}

async function injectFingerprint(page, fingerprint) {
  if (!fingerprint) return;
  try {
    const { FingerprintInjector } = require("fingerprint-injector");
    const injector = new FingerprintInjector();
    await injector.injectFingerprintToPlaywright(page, fingerprint);
    debugLog("fingerprint injected");
  } catch {
    debugLog("warning: fingerprint-injector failed — skipping");
  }
}

// ─── browser session ──────────────────────────────────────────────────────────

async function launchBrowserSession(chromium, fingerprint, timeout) {
  const profileDir = path.resolve(
    process.env.BLINKIT_BROWSER_PROFILE_DIR || ".data/blinkit-browser-profile"
  );
  fs.mkdirSync(profileDir, { recursive: true });

  const headless = parseBool(process.env.BLINKIT_BROWSER_HEADLESS, true);
  const executablePath = String(process.env.BLINKIT_BROWSER_EXECUTABLE_PATH || "").trim() || undefined;

  const launchOptions = {
    headless,
    locale: "en-IN",
    timezoneId: "Asia/Kolkata",
    viewport: { width: 390, height: 844 },
    userAgent: fingerprint?.navigator?.userAgent ||
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0.6099.119 Mobile/15E148 Safari/604.1",
    args: [
      "--no-sandbox",
      "--disable-setuid-sandbox",
      "--disable-blink-features=AutomationControlled",
      "--disable-dev-shm-usage",
      "--disable-gpu",
      "--no-first-run",
    ],
  };

  if (executablePath) launchOptions.executablePath = executablePath;

  const proxyServer = String(process.env.BLINKIT_BROWSER_PROXY_SERVER || "").trim();
  if (proxyServer) {
    launchOptions.proxy = { server: proxyServer };
    const u = String(process.env.BLINKIT_BROWSER_PROXY_USERNAME || "").trim();
    const p = String(process.env.BLINKIT_BROWSER_PROXY_PASSWORD || "").trim();
    if (u) launchOptions.proxy.username = u;
    if (p) launchOptions.proxy.password = p;
  }

  let context;
  try {
    context = await chromium.launchPersistentContext(profileDir, launchOptions);
  } catch (err) {
    const mapped = displayError(profileLockError(err, profileDir));
    if (!localStatePrefsError(err) && !localStatePrefsError(mapped)) throw mapped;
    if (!resetCorruptLocalState(profileDir)) throw mapped;
    debugLog("retrying launch after Local State reset");
    try {
      context = await chromium.launchPersistentContext(profileDir, launchOptions);
    } catch (retryErr) {
      throw displayError(profileLockError(retryErr, profileDir));
    }
  }
  context.setDefaultTimeout(timeout);
  context.setDefaultNavigationTimeout(timeout);

  // Inject fingerprint before any navigation.
  const page = context.pages()[0] ?? (await context.newPage());
  await injectFingerprint(page, fingerprint);

  // Inject saved cookies from file if configured.
  await injectSavedCookies(context);

  return { context, page, headless };
}

// ─── cookie injection ─────────────────────────────────────────────────────────

async function injectSavedCookies(context) {
  const cookiesFile = String(process.env.BLINKIT_BROWSER_COOKIES_FILE || "").trim();
  if (!cookiesFile) return;
  try {
    const raw = fs.readFileSync(cookiesFile, "utf8");
    const cookies = JSON.parse(raw);
    if (Array.isArray(cookies) && cookies.length > 0) {
      await context.addCookies(cookies);
      debugLog(`injected ${cookies.length} cookies from ${cookiesFile}`);
    }
  } catch (err) {
    debugLog(`warning: could not inject cookies: ${cleanError(err)}`);
  }
}

// ─── geolocation ──────────────────────────────────────────────────────────────

async function applyGeolocation(context, lat, lng) {
  if (lat === null || lng === null) return;
  try {
    await context.grantPermissions(["geolocation"], { origin: "https://blinkit.com" });
    await context.setGeolocation({ latitude: lat, longitude: lng, accuracy: 50 });
  } catch (err) {
    debugLog(`geolocation failed: ${cleanError(err)}`);
  }
}

// ─── curl-impersonate direct API call ─────────────────────────────────────────

async function trySearchWithCurlImpersonate(query, lat, lng, pageCookies, timeout) {
  const binary = String(process.env.BLINKIT_CURL_IMPERSONATE || "").trim();
  if (!binary) return null;
  const url = `https://blinkit.com${SEARCH_PATH}?q=${encodeURIComponent(query)}&search_type=type_to_search`;
  const headers = { accept: "application/json,text/plain,*/*", app_client: "consumer_web", platform: "mobile_web", web_app_version: "1008010016" };
  if (lat !== null) { headers.lat = String(lat); headers.cur_lat = String(lat); }
  if (lng !== null) { headers.lon = String(lng); headers.cur_lon = String(lng); }
  const args = [url, "--silent", "--max-time", String(Math.ceil(timeout / 1000)), "--compressed"];
  for (const [k, v] of Object.entries(headers)) args.push("-H", `${k}: ${v}`);
  if (pageCookies.length > 0) args.push("-H", `Cookie: ${pageCookies.map((c) => `${c.name}=${c.value}`).join("; ")}`);
  try {
    const { stdout } = await execFileAsync(binary, args, { timeout: timeout + 5000 });
    return { ok: true, url, status: 200, raw: JSON.parse(stdout.trim()) };
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

async function waitForSearchFromUrl(page, targetUrl, timeout) {
  const pending = page.waitForResponse(isSearchResponse, { timeout }).catch(() => null);
  await page.goto(targetUrl, { waitUntil: "domcontentloaded", timeout });
  return pending;
}

async function waitForSearchFromHome(page, query, timeout) {
  const pending = page.waitForResponse(isSearchResponse, { timeout }).catch(() => null);
  await page.goto("https://blinkit.com/", { waitUntil: "domcontentloaded", timeout });
  const candidates = ['input[type="search"]', 'input[placeholder*="Search"]', 'input[placeholder*="search"]', 'input[aria-label*="Search"]', 'input[name="q"]'];
  let input = null;
  for (const sel of candidates) {
    const loc = page.locator(sel).first();
    if ((await loc.count()) > 0) { input = loc; break; }
  }
  if (!input) return pending;
  await input.click({ timeout: 5000 });
  await input.fill(query, { timeout: 5000 });
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
      const readLS = (...keys) => { for (const k of keys) { const v = window.localStorage.getItem(k); if (typeof v === "string" && v.trim()) return v.trim(); } return ""; };
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
    if (raw.status === 403 && textContainsChallenge(raw.text)) throw new Error("human_verification_required: anti-bot challenge page returned 403");
    const snippet = typeof raw.text === "string" ? raw.text.replace(/\s+/g, " ").trim().slice(0, 180) : "";
    throw new Error(snippet ? `in-page fetch status ${raw.status} (${raw.url}): ${snippet}` : `in-page fetch status ${raw.status} (${raw.url})`);
  }
  try { return { url: raw.url, status: raw.status, body: JSON.parse(raw.text) }; } catch { throw new Error("in-page fetch returned non-JSON response"); }
}

// ─── helpers ──────────────────────────────────────────────────────────────────

function toOptionalNumber(value) {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") { const p = Number.parseFloat(value); if (Number.isFinite(p)) return p; }
  return null;
}

function print(payload) { process.stdout.write(`${JSON.stringify(payload)}\n`); }
function cleanError(err) { if (!err) return "unknown error"; return (typeof err === "string" ? err : err.message || String(err)).replace(/\s+/g, " ").trim(); }
function localStatePrefsError(err) {
  const lower = cleanError(err).toLowerCase();
  return lower.includes("failed to read prefs") && lower.includes("local state");
}
function resetCorruptLocalState(profileDir) {
  const localState = path.join(profileDir, "Local State");
  if (!fs.existsSync(localState)) return false;
  try {
    const backup = `${localState}.corrupt-${Date.now()}`;
    fs.renameSync(localState, backup);
    debugLog(`moved corrupt Local State file to ${backup}`);
    return true;
  } catch (err) {
    debugLog(`warning: failed to rotate Local State file: ${cleanError(err)}`);
    return false;
  }
}
function displayError(err) {
  const msg = cleanError(err);
  const lower = msg.toLowerCase();
  if (!lower.includes("missing x server") && !lower.includes("without having a xserver running") && !lower.includes("$display")) {
    return err;
  }
  const snippet = msg.length > 320 ? `${msg.slice(0, 320)}...` : msg;
  return new Error(
    "headed browser requires a display server, but this VPS has no X server. " +
    "Set BLINKIT_BROWSER_HEADLESS=true for server usage. " +
    "If you need headed mode, run with xvfb (xvfb-run -a node scripts/blinkit-browser-search.mjs ...). " +
    `Original error: ${snippet}`
  );
}
function profileLockError(err, profileDir) {
  const msg = cleanError(err);
  const lower = msg.toLowerCase();
  if (!lower.includes("processsingleton") && !lower.includes("singletonlock")) return err;
  const lockFiles = [
    path.join(profileDir, "SingletonLock"),
    path.join(profileDir, "SingletonSocket"),
    path.join(profileDir, "SingletonCookie"),
  ];
  const snippet = msg.length > 320 ? `${msg.slice(0, 320)}...` : msg;
  return new Error(
    `browser profile lock detected for ${profileDir}. ` +
    "Another Chromium/worker instance is using this profile. " +
    "Stop blinkit-browser-worker (systemctl stop blinkit-browser-worker) and close any other Chromium using this profile, then retry. " +
    `If no Chromium process is running, remove stale lock files and retry: rm -f ${lockFiles.join(" ")}. ` +
    `Original error: ${snippet}`
  );
}
function textContainsChallenge(text) { if (typeof text !== "string" || !text.trim()) return false; const lower = text.toLowerCase(); return CHALLENGE_MARKERS.some((m) => lower.includes(m)); }
function writeJSON(res, status, payload) { res.statusCode = status; res.setHeader("Content-Type", "application/json"); res.end(JSON.stringify(payload)); }
async function readJSONBody(req) { const chunks = []; for await (const chunk of req) chunks.push(chunk); const text = Buffer.concat(chunks).toString("utf8"); if (!text.trim()) return {}; return JSON.parse(text); }
function enqueueTask(state, task) { const next = state.queue.then(task, task); state.queue = next.then(() => undefined, () => undefined); return next; }

function splitSearchBudget(totalMs) {
  const timeout = Number.isFinite(totalMs) && totalMs > 0 ? totalMs : DEFAULT_TIMEOUT_MS;
  const min = 1000;
  let directMs = Math.floor(timeout * 0.5), homeMs = Math.floor(timeout * 0.3), fetchMs = timeout - directMs - homeMs;
  if (directMs < min) directMs = min;
  if (homeMs < min) homeMs = min;
  if (fetchMs < min) { const d = min - fetchMs; const ch = Math.min(d, Math.max(0, homeMs - min)); homeMs -= ch; const cd = Math.min(d - ch, Math.max(0, directMs - min)); directMs -= cd; fetchMs = timeout - directMs - homeMs; if (fetchMs < min) fetchMs = min; }
  return { directMs, homeMs, fetchMs };
}

async function pageLooksLikeChallenge(page) {
  try { const s = await page.evaluate(() => `${document.title}\n${((document.body && document.body.innerText) || "").slice(0, 4000)}`); return textContainsChallenge(s); } catch { return false; }
}

// ─── DOM product extraction ───────────────────────────────────────────────────

async function extractProductsFromDOM(page, query) {
  return page.evaluate(({ query }) => {
    const normalize = (v) => (typeof v === "string" ? v.trim() : "");
    const toNumber = (v) => { if (typeof v === "number" && Number.isFinite(v)) return v; if (typeof v === "string") { const c = v.replace(/[^\d.]/g, ""); if (c) { const p = Number.parseFloat(c); if (Number.isFinite(p)) return p; } } return null; };
    const toBool = (v, fb = true) => { if (typeof v === "boolean") return v; if (typeof v === "number") return v > 0; if (typeof v === "string") { const l = v.toLowerCase(); if (["true","yes","in_stock","available"].includes(l)) return true; if (["false","no","out_of_stock","unavailable"].includes(l)) return false; } return fb; };
    const mapStr = (o, keys) => { for (const k of keys) { const v = normalize(o?.[k]); if (v) return v; } return ""; };
    const mapNum = (o, keys) => { for (const k of keys) { const v = toNumber(o?.[k]); if (v !== null) return v; } return null; };
    const seen = new Set(); const products = [];
    const append = (c) => { const id = normalize(c.id || c.product_id); const name = normalize(c.name || c.title || c.product_name); const price = toNumber(c.price ?? c.selling_price ?? c.final_price); if (!name || price === null) return; const key = id || `${name.toLowerCase()}::${price}`; if (seen.has(key)) return; seen.add(key); products.push({ id: id || key, name, brand: normalize(c.brand), price, mrp: toNumber(c.mrp ?? c.original_price) ?? price, unit: normalize(c.unit || c.quantity), store_id: normalize(c.store_id || c.storeId), in_stock: toBool(c.in_stock ?? c.is_available ?? c.available ?? c.stock, true) }); };
    const walk = (v) => { if (!v) return; if (Array.isArray(v)) { v.forEach(walk); return; } if (typeof v !== "object") return; append({ id: mapStr(v,["id","product_id","variant_id","sku","entity_id"]), name: mapStr(v,["name","title","product_name"]), brand: mapStr(v,["brand","brand_name"]), price: mapNum(v,["price","selling_price","final_price","sp"]), mrp: mapNum(v,["mrp","original_price","list_price"]), unit: mapStr(v,["unit","quantity","pack_size"]), in_stock: v.in_stock??v.available??v.is_available??v.stock, store_id: mapStr(v,["store_id","storeId","store"]) }); Object.values(v).forEach(walk); };
    for (const s of Array.from(document.querySelectorAll("script"))) { const t = (s.textContent||"").trim(); if (!t||t.length>1_500_000) continue; if (!t.startsWith("{")||!t.startsWith("[")) { try { walk(JSON.parse(t)); } catch {} } }
    try { if (window.__NEXT_DATA__) walk(window.__NEXT_DATA__); } catch {}
    try { if (window.__INITIAL_STATE__) walk(window.__INITIAL_STATE__); } catch {}
    if (products.length === 0) { for (const card of Array.from(document.querySelectorAll('[data-testid*="product"],[class*="product"],[class*="Product"]')).slice(0,80)) { const text=(card.textContent||"").replace(/\s+/g," ").trim(); if(!text) continue; const pm=text.match(/(?:₹|Rs\.?)\s*([0-9]+(?:\.[0-9]+)?)/i); if(!pm) continue; const nl=text.split(/₹|Rs\.?/i)[0].replace(/\s{2,}/g," ").trim(); if(!nl) continue; append({id:"",name:nl,price:pm[1],mrp:pm[1],in_stock:!/out of stock/i.test(text)}); } }
    const q = normalize(query).toLowerCase();
    return products.map((p) => { const n=normalize(p.name).toLowerCase(); const score=q&&n.includes(q)?2:q&&q.split(/\s+/).some((t)=>n.includes(t))?1:0; return{score,product:p}; }).sort((a,b)=>b.score-a.score||a.product.price-b.product.price).map((x)=>x.product).slice(0,40);
  }, { query });
}

// ─── search orchestration ─────────────────────────────────────────────────────

async function runSearch(context, page, query, lat, lng, timeout) {
  const budget = splitSearchBudget(timeout);
  await applyGeolocation(context, lat, lng);

  // curl-impersonate path (correct TLS fingerprint, no browser overhead).
  const pageCookies = await context.cookies("https://blinkit.com").catch(() => []);
  const curlResult = await trySearchWithCurlImpersonate(query, lat, lng, pageCookies, budget.directMs);
  if (curlResult) return curlResult;

  const searchURL = `https://blinkit.com/s/?q=${encodeURIComponent(query)}`;
  let response = await waitForSearchFromUrl(page, searchURL, budget.directMs);

  if (!response && (await pageLooksLikeChallenge(page))) {
    return { ok: false, error: "human_verification_required: blinkit anti-bot challenge detected" };
  }
  if (!response) {
    debugLog("direct search miss, trying home fallback");
    response = await waitForSearchFromHome(page, query, budget.homeMs);
  }
  if (response) {
    debugLog(`search response captured status=${response.status()}`);
    return { ok: true, url: response.url(), status: response.status(), raw: await response.json() };
  }
  if (await pageLooksLikeChallenge(page)) {
    return { ok: false, error: "human_verification_required: blinkit anti-bot challenge detected" };
  }
  const domProducts = await extractProductsFromDOM(page, query);
  if (domProducts.length > 0) {
    debugLog(`dom extraction yielded products=${domProducts.length}`);
    return { ok: true, url: page.url(), status: 200, raw: { products: domProducts, source: "dom" } };
  }
  debugLog("network miss; using in-page fetch fallback");
  const fallback = await fetchSearchViaPage(page, query, lat, lng, budget.fetchMs);
  return { ok: true, url: fallback.url, status: fallback.status, raw: fallback.body };
}

// ─── session snapshot ─────────────────────────────────────────────────────────

async function sessionStatusSnapshot(page) {
  try {
    return await page.evaluate(() => {
      const safeGet = (...keys) => { for (const k of keys) { try { const v = window.localStorage.getItem(k); if (typeof v === "string" && v.trim()) return v.trim(); } catch {} } return ""; };
      return { title: document.title || "", body: ((document.body && document.body.innerText) || "").slice(0, 4000), accessTokenPresent: Boolean(safeGet("gr_1_accessToken", "access_token", "accessToken")), authKeyPresent: Boolean(safeGet("auth_key", "gr_1_authKey")) };
    });
  } catch {
    return { title: "", body: "", accessTokenPresent: false, authKeyPresent: false };
  }
}

function decodeMaybe(v) { if (typeof v !== "string") return ""; try { return decodeURIComponent(v); } catch { return v; } }
function firstNonEmpty(...vals) { for (const v of vals) if (typeof v === "string" && v.trim()) return v.trim(); return ""; }
function findTokenByKey(store, cookies, keys) { const norm = new Set(keys.map((k) => k.toLowerCase())); for (const [k, v] of Object.entries(store)) if (norm.has(k.toLowerCase()) && String(v).trim()) return String(v).trim(); for (const c of cookies) if (norm.has(String(c.name).toLowerCase())) { const v = decodeMaybe(c.value); if (v.trim()) return v.trim(); } return ""; }
function findAccessTokenFallback(store, cookies) { const cands = [...Object.values(store).filter((v) => typeof v === "string"), ...cookies.map((c) => decodeMaybe(c.value))]; return cands.find((v) => v.startsWith("v2::")) || ""; }
function safePageURL(page) { try { return page.url(); } catch { return ""; } }
function workerStatusFromSnapshot(page, snapshot) {
  const title = typeof snapshot?.title === "string" ? snapshot.title : "";
  const body = typeof snapshot?.body === "string" ? snapshot.body : "";
  return {
    ok: true,
    page_url: safePageURL(page),
    title,
    access_token_present: Boolean(snapshot?.accessTokenPresent),
    auth_key_present: Boolean(snapshot?.authKeyPresent),
    challenge_detected: textContainsChallenge(`${title}\n${body}`),
  };
}
async function refreshWorkerStatusCache(state) {
  const snapshot = await sessionStatusSnapshot(state.page);
  state.status = workerStatusFromSnapshot(state.page, snapshot);
  return state.status;
}
function updateWorkerStatusCache(state, snapshot) {
  state.status = workerStatusFromSnapshot(state.page, snapshot);
  return state.status;
}

async function readSessionSnapshot(context, page) {
  const storage = await page.evaluate(() => {
    const out = {}; const len = (() => { try { return window.localStorage.length; } catch { return 0; } })();
    for (let i = 0; i < len; i++) { const k = (() => { try { return window.localStorage.key(i); } catch { return ""; } })(); if (!k) continue; out[k] = (() => { try { return window.localStorage.getItem(k); } catch { return ""; } })() || ""; }
    return { localStorage: out, userAgent: navigator.userAgent || "" };
  }).catch(() => ({ localStorage: {}, userAgent: "" }));
  const cookies = await context.cookies("https://blinkit.com").catch(() => []);
  const ls = storage.localStorage || {};
  const accessToken = firstNonEmpty(findTokenByKey(ls, cookies, ["gr_1_accessToken", "access_token", "accessToken"]), findAccessTokenFallback(ls, cookies));
  const refreshToken = firstNonEmpty(findTokenByKey(ls, cookies, ["auth_key", "refresh_token", "refreshToken", "gr_1_authKey", "gr_1_refreshToken"]), accessToken);
  return { accessToken, refreshToken, userAgent: storage.userAgent || "" };
}

function uniqueEntities(rows, getKey) {
  const out = []; const seen = new Set();
  for (const row of Array.isArray(rows) ? rows : []) {
    const key = String(getKey(row) || "").trim();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push(row);
  }
  return out;
}

async function fetchBootstrapMetadata(page) {
  const payload = await page.evaluate(async () => {
    const decodeMaybe = (v) => { try { return decodeURIComponent(v); } catch { return v; } };
    const safeGet = (o, ...keys) => { for (const k of keys) { const v = o?.[k]; if (typeof v === "string" && v.trim()) return v.trim(); if (typeof v === "number" && Number.isFinite(v)) return String(v); } return ""; };
    const safeNum = (o, ...keys) => { for (const k of keys) { const v = o?.[k]; if (typeof v === "number" && Number.isFinite(v)) return v; if (typeof v === "string" && v.trim()) { const p = Number.parseFloat(v); if (Number.isFinite(p)) return p; } } return null; };
    const safeBool = (o, ...keys) => { for (const k of keys) { const v = o?.[k]; if (typeof v === "boolean") return v; if (typeof v === "number") return v > 0; if (typeof v === "string") { const lower = v.trim().toLowerCase(); if (["true", "yes", "1", "default"].includes(lower)) return true; if (["false", "no", "0"].includes(lower)) return false; } } return false; };
    const cookieMap = () => {
      const out = {};
      for (const entry of (document.cookie || "").split(";")) {
        const part = entry.trim(); if (!part) continue;
        const idx = part.indexOf("=");
        if (idx <= 0) continue;
        out[part.slice(0, idx).trim()] = part.slice(idx + 1).trim();
      }
      return out;
    };
    const localStore = () => {
      const out = {}; const len = (() => { try { return window.localStorage.length; } catch { return 0; } })();
      for (let i = 0; i < len; i++) {
        const key = (() => { try { return window.localStorage.key(i); } catch { return ""; } })();
        if (!key) continue;
        out[key] = (() => { try { return window.localStorage.getItem(key); } catch { return ""; } })() || "";
      }
      return out;
    };
    const ls = localStore();
    const cookies = cookieMap();
    const values = [...Object.values(ls), ...Object.values(cookies).map((v) => decodeMaybe(v))].filter((v) => typeof v === "string");
    const findByKeys = (...keys) => {
      const norm = new Set(keys.map((k) => k.toLowerCase()));
      for (const [k, v] of Object.entries(ls)) if (norm.has(k.toLowerCase()) && typeof v === "string" && v.trim()) return v.trim();
      for (const [k, v] of Object.entries(cookies)) if (norm.has(k.toLowerCase())) { const d = decodeMaybe(v); if (d.trim()) return d.trim(); }
      return "";
    };
    const accessToken = findByKeys("gr_1_accessToken", "access_token", "accessToken") || values.find((v) => v.startsWith("v2::")) || "";
    const authKey = findByKeys("auth_key", "refresh_token", "refreshToken", "gr_1_authKey", "gr_1_refreshToken") || accessToken;
    const headers = { accept: "application/json,text/plain,*/*", app_client: "consumer_web", platform: "mobile_web" };
    if (accessToken) headers.access_token = accessToken;
    if (authKey) headers.auth_key = authKey;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10000);
    const parseMaybeJSON = (raw) => {
      if (typeof raw !== "string") return null;
      const text = raw.trim();
      if (!text || text.length > 500000) return null;
      if (!(text.startsWith("{") || text.startsWith("["))) return null;
      try { return JSON.parse(text); } catch { return null; }
    };
    const walk = (value, visit) => {
      if (!value) return;
      if (Array.isArray(value)) { for (const item of value) walk(item, visit); return; }
      if (typeof value !== "object") return;
      visit(value);
      for (const item of Object.values(value)) walk(item, visit);
    };
    const addressFromObject = (obj) => {
      const id = safeGet(obj, "id", "address_id", "addressId");
      const label = safeGet(obj, "label", "name", "tag", "title");
      const full = safeGet(obj, "full_address", "fullAddress", "address", "display_address", "displayAddress");
      const lat = safeNum(obj, "lat", "latitude");
      const lng = safeNum(obj, "lng", "lon", "longitude");
      if (!id) return null;
      if (!full && !label && lat === null && lng === null) return null;
      return {
        id,
        label,
        full_address: full || label,
        lat,
        lng,
        is_default: safeBool(obj, "is_default", "default"),
      };
    };
    const collectRoots = (seed) => {
      const roots = [];
      const push = (value) => { if (value && typeof value === "object") roots.push(value); };
      push(seed);
      try { push(window.__NEXT_DATA__); } catch {}
      try { push(window.__INITIAL_STATE__); } catch {}
      for (const value of Object.values(ls)) {
        const parsed = parseMaybeJSON(value);
        if (parsed) push(parsed);
      }
      for (const script of Array.from(document.scripts).slice(0, 80)) {
        const parsed = parseMaybeJSON(script.textContent || "");
        if (parsed) push(parsed);
      }
      return roots;
    };
    try {
      const addresses = [];
      const resp = await fetch("/api/v1/config/primary?fetch_nearest_addresses=true", {
        method: "GET",
        credentials: "include",
        headers,
        signal: controller.signal,
      });
      const json = resp.ok ? await resp.json().catch(() => null) : null;
      for (const root of collectRoots(json)) {
        walk(root, (obj) => {
          const address = addressFromObject(obj);
          if (address) addresses.push(address);
        });
      }
      return { addresses, payments: [] };
    } catch {
      return { addresses: [], payments: [] };
    } finally {
      clearTimeout(timer);
    }
  }).catch(() => ({ addresses: [], payments: [] }));

  return {
    addresses: uniqueEntities((payload && payload.addresses) || [], (row) => row.id).map((row, index) => ({
      id: String(row.id || "").trim(),
      label: String(row.label || "").trim(),
      full_address: String(row.full_address || row.label || "").trim(),
      lat: typeof row.lat === "number" && Number.isFinite(row.lat) ? row.lat : 0,
      lng: typeof row.lng === "number" && Number.isFinite(row.lng) ? row.lng : 0,
      is_default: index === 0 ? true : Boolean(row.is_default),
    })).filter((row) => row.id && (row.full_address || row.label)),
    payments: uniqueEntities((payload && payload.payments) || [], (row) => row.id || row.token).map((row) => ({
      id: String(row.id || "").trim(),
      label: String(row.label || "").trim(),
      token: String(row.token || "").trim(),
      type: String(row.type || "").trim(),
      is_default: Boolean(row.is_default),
    })).filter((row) => row.token),
  };
}

function buildBootstrapResult(snapshot, metadata = {}) {
  const now = new Date();
  return {
    ok: true,
    session: {
      access_token: snapshot.accessToken,
      refresh_token: snapshot.refreshToken,
      token_expires_at: new Date(now.getTime() + 7 * 24 * 60 * 60 * 1000).toISOString(),
      device_headers: { app_client: "consumer_web", platform: "mobile_web", "user-agent": snapshot.userAgent || "" },
      addresses: Array.isArray(metadata.addresses) ? metadata.addresses : [],
      payments: Array.isArray(metadata.payments) ? metadata.payments : [],
    },
  };
}

// ─── bootstrap flow ───────────────────────────────────────────────────────────

async function runBootstrapFlow(context, page, timeout, onStatus = null) {
  const challengeError = "human_verification_required: blinkit anti-bot challenge detected";
  const deadline = newDeadline(timeout);
  await page.goto("https://blinkit.com/", { waitUntil: "domcontentloaded", timeout });
  let status = await sessionStatusSnapshot(page);
  if (onStatus) onStatus(status);
  if (textContainsChallenge(`${status.title}\n${status.body}`)) return { ok: false, error: challengeError };
  let snapshot = await readSessionSnapshot(context, page);
  if (snapshot.accessToken) return buildBootstrapResult(snapshot, await fetchBootstrapMetadata(page));
  debugLog("bootstrap awaiting login/session in persistent profile");
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 2000));
    status = await sessionStatusSnapshot(page);
    if (onStatus) onStatus(status);
    if (textContainsChallenge(`${status.title}\n${status.body}`)) return { ok: false, error: challengeError };
    snapshot = await readSessionSnapshot(context, page);
    if (snapshot.accessToken) return buildBootstrapResult(snapshot, await fetchBootstrapMetadata(page));
  }
  status = await sessionStatusSnapshot(page);
  if (onStatus) onStatus(status);
  if (textContainsChallenge(`${status.title}\n${status.body}`)) return { ok: false, error: challengeError };
  return { ok: false, error: `access token not found; complete Blinkit login and retry bootstrap${status.title ? `; page_title="${status.title}"` : ""}` };
}

// ─── runners ──────────────────────────────────────────────────────────────────

async function runOneShot(args) {
  const timeout = timeoutMs();
  const chromium = loadChromium();
  const fingerprint = generateFingerprint();
  const { context, page, headless } = await launchBrowserSession(chromium, fingerprint, timeout);
  debugLog(`search start mode=oneshot headless=${headless} timeout_ms=${timeout}`);
  try { print(await runSearch(context, page, args.query, args.lat, args.lng, timeout)); } finally { await context.close().catch(() => {}); }
}

async function runBootstrapOneShot() {
  const timeout = bootstrapTimeoutMs();
  const chromium = loadChromium();
  const fingerprint = generateFingerprint();
  const { context, page, headless } = await launchBrowserSession(chromium, fingerprint, timeout);
  debugLog(`bootstrap start mode=oneshot headless=${headless} timeout_ms=${timeout}`);
  try { print(await runBootstrapFlow(context, page, timeout)); } finally { await context.close().catch(() => {}); }
}

async function runWorker(args) {
  const timeout = timeoutMs();
  const chromium = loadChromium();
  const fingerprint = generateFingerprint();
  const { context, page, headless } = await launchBrowserSession(chromium, fingerprint, timeout);
  const bind = workerBind(args);
  const port = workerPort(args);
  const state = {
    queue: Promise.resolve(),
    context,
    page,
    status: { ok: true, page_url: safePageURL(page), title: "", access_token_present: false, auth_key_present: false, challenge_detected: false },
  };
  await refreshWorkerStatusCache(state).catch(() => {});

  const server = http.createServer(async (req, res) => {
    if (req.method === "GET" && req.url === "/health") { writeJSON(res, 200, { status: "ok" }); return; }
    if (req.method === "GET" && req.url === "/status") {
      writeJSON(res, 200, state.status);
      return;
    }
    if (req.method !== "POST" || (req.url !== "/search" && req.url !== "/bootstrap")) { writeJSON(res, 404, { ok: false, error: "not found" }); return; }
    let body = {};
    try { body = await readJSONBody(req); } catch (err) { writeJSON(res, 400, { ok: false, error: `invalid JSON: ${cleanError(err)}` }); return; }
    try {
      const result = await enqueueTask(state, async () => {
        await refreshWorkerStatusCache(state).catch(() => {});
        if (req.url === "/search") {
          const query = String(body.query || "").trim();
          if (!query) throw new Error("query is required");
          const st = searchTimeoutMs(body);
          debugLog(`search mode=worker timeout_ms=${st} query=${query}`);
          try { return await runSearch(state.context, state.page, query, toOptionalNumber(body.lat), toOptionalNumber(body.lng), st); }
          finally { await refreshWorkerStatusCache(state).catch(() => {}); }
        }
        const bt = bootstrapTimeoutMs(body);
        debugLog(`bootstrap mode=worker timeout_ms=${bt}`);
        try {
          return await runBootstrapFlow(state.context, state.page, bt, (snapshot) => updateWorkerStatusCache(state, snapshot));
        } finally { await refreshWorkerStatusCache(state).catch(() => {}); }
      });
      writeJSON(res, 200, result);
    } catch (err) { writeJSON(res, 200, { ok: false, error: cleanError(err) }); }
  });

  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(port, bind, resolve); });
  debugLog(`worker listening on http://${bind}:${port}`);
  const shutdown = async (signal) => { debugLog(`shutdown signal=${signal}`); server.close(() => {}); try { await context.close(); } catch {} process.exit(0); };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
  await new Promise(() => {});
}

// ─── entry point ──────────────────────────────────────────────────────────────

try {
  const args = parseArgs(process.argv.slice(2));
  if (args.worker) { await runWorker(args); }
  else if (args.bootstrap) { await runBootstrapOneShot(); }
  else { await runOneShot(args); }
} catch (err) {
  print({ ok: false, error: cleanError(err) });
  process.exitCode = 1;
}
