#!/usr/bin/env node

import process from "node:process";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);

function parseBool(raw, fallback) {
  if (raw === undefined) return fallback;
  const v = String(raw).trim().toLowerCase();
  if (v === "1" || v === "true" || v === "yes") return true;
  if (v === "0" || v === "false" || v === "no") return false;
  return fallback;
}

function parseTimeout(raw, fallback) {
  if (!raw) return fallback;
  const n = Number.parseInt(String(raw), 10);
  if (!Number.isFinite(n) || n <= 0) return fallback;
  return n;
}

function cleanError(err) {
  if (!err) return "unknown error";
  if (typeof err === "string") return err;
  if (err && typeof err.message === "string" && err.message.trim()) return err.message.trim();
  return String(err);
}

function loadChromium() {
  try {
    const playwrightExtra = require("playwright-extra");
    const chromium = playwrightExtra.chromium;
    try {
      const StealthPlugin = require("puppeteer-extra-plugin-stealth");
      chromium.use(StealthPlugin());
    } catch {
      // Keep smoke test functional even if stealth plugin is missing.
    }
    return chromium;
  } catch {
    throw new Error("playwright-extra is not installed. Run: npm install");
  }
}

async function main() {
  const chromium = loadChromium();
  const headless = parseBool(process.env.BLINKIT_BROWSER_HEADLESS, true);
  const timeout = parseTimeout(process.env.BLINKIT_BROWSER_TIMEOUT_MS, 30000);
  const executablePath = String(process.env.BLINKIT_BROWSER_EXECUTABLE_PATH || "").trim() || undefined;

  const launchOptions = {
    headless,
    timeout,
    args: [
      "--no-sandbox",
      "--disable-setuid-sandbox",
      "--disable-dev-shm-usage",
      "--disable-gpu",
      "--no-zygote",
      "--no-first-run",
    ],
  };
  if (executablePath) launchOptions.executablePath = executablePath;

  const browser = await chromium.launch(launchOptions);
  try {
    const page = await browser.newPage();
    const result = await page.evaluate(() => "hello");
    const browserVersion = typeof browser.version === "function" ? await browser.version() : "";
    process.stdout.write(
      `${JSON.stringify({
        ok: true,
        result,
        headless,
        browser_version: browserVersion,
        executable_path: executablePath || "playwright-managed",
      })}\n`
    );
  } finally {
    await browser.close().catch(() => {});
  }
}

main().catch((err) => {
  process.stderr.write(`[chrome-smoke] ${cleanError(err)}\n`);
  process.exitCode = 1;
});
