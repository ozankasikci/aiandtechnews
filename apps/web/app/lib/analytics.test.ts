import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";
import { sanitizePagePath, sanitizeSearchTerm, trackNewsletterSignup } from "./analytics";
import { GA_ID, GA_INIT_SCRIPT } from "./googleAnalytics";

test("normalizes search terms before analytics collection", () => {
  assert.equal(sanitizeSearchTerm("  open   source AI  "), "open source AI");
});

test("redacts search terms that may contain personal contact information", () => {
  assert.equal(sanitizeSearchTerm("person@example.com"), "[redacted]");
  assert.equal(sanitizeSearchTerm("https://example.com/private"), "[redacted]");
});

test("sanitizes query values before they reach page_view events", () => {
  assert.equal(
    sanitizePagePath("/search", new URLSearchParams("q=person%40example.com")),
    "/search?q=%5Bredacted%5D",
  );
  assert.equal(
    sanitizePagePath("/search", new URLSearchParams("q=open+source+AI")),
    "/search?q=open+source+AI",
  );
});

test("drops empty query values and keeps plain paths unchanged", () => {
  assert.equal(sanitizePagePath("/search", new URLSearchParams("q=+")), "/search");
  assert.equal(sanitizePagePath("/about", new URLSearchParams()), "/about");
});

test("counts newly activated newsletter subscribers as leads", () => {
  const calls: unknown[][] = [];
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: { gtag: (...args: unknown[]) => calls.push(args) },
  });

  try {
    trackNewsletterSignup("footer", "subscribed");
    assert.deepEqual(calls, [
      ["event", "newsletter_signup_requested", { method: "newsletter", placement: "footer" }],
      ["event", "generate_lead", { method: "newsletter", placement: "footer" }],
    ]);

    calls.length = 0;
    trackNewsletterSignup("footer", "already_active");
    assert.deepEqual(calls, [
      ["event", "newsletter_signup_requested", { method: "newsletter", placement: "footer" }],
    ]);
  } finally {
    if (previousWindow) Object.defineProperty(globalThis, "window", previousWindow);
    else Reflect.deleteProperty(globalThis, "window");
  }
});

test("queues a successful signup before the Google tag library loads", () => {
  const browserWindow: Record<string, unknown> = {};
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  runInNewContext(GA_INIT_SCRIPT, { window: browserWindow, Date });
  Object.defineProperty(globalThis, "window", { configurable: true, value: browserWindow });

  try {
    trackNewsletterSignup("footer", "subscribed");
    const calls = Array.from(browserWindow.dataLayer as IArguments[], (args) =>
      JSON.parse(JSON.stringify(Array.from(args))) as unknown[],
    );
    assert.equal(calls[0][0], "js");
    assert.deepEqual(calls[1], ["config", GA_ID, { send_page_view: false }]);
    assert.deepEqual(calls.slice(2), [
      ["event", "newsletter_signup_requested", { method: "newsletter", placement: "footer" }],
      ["event", "generate_lead", { method: "newsletter", placement: "footer" }],
    ]);
  } finally {
    if (previousWindow) Object.defineProperty(globalThis, "window", previousWindow);
    else Reflect.deleteProperty(globalThis, "window");
  }
});

test("initializes the analytics queue before page hydration", () => {
  const layout = readFileSync(new URL("../layout.tsx", import.meta.url), "utf8");
  assert.match(layout, /<Script id="ga-init" strategy="beforeInteractive">/);
  assert.match(layout, /<Script src=\{`https:\/\/www\.googletagmanager\.com\/gtag\/js\?id=\$\{GA_ID\}`\} strategy="afterInteractive"/);
});
