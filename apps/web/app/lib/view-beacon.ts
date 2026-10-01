// Article views are counted by the API only when a reader's browser reports
// them: article pages are cached (ISR), so the server-side read no longer
// happens per reader. The beacon is a CORS "simple" request (sendBeacon with
// a text/plain body, or a no-cors keepalive fetch), so no preflight is needed.

export function articleViewUrl(apiBase: string, slug: string) {
  return `${apiBase.replace(/\/+$/, "")}/api/articles/${encodeURIComponent(slug)}/view`;
}

export interface BeaconEnvironment {
  webdriver?: boolean;
  userAgent?: string;
  sendBeacon?: (url: string, data?: BodyInit | null) => boolean;
  fetch?: typeof fetch;
}

const BOT_USER_AGENT = /bot|crawl|spider|slurp|preview|headless|lighthouse|phantomjs|puppeteer|playwright/i;

// Sends one view for slug. Returns false when the environment looks
// automated or nothing could be sent.
export function sendArticleView(apiBase: string, slug: string, env: BeaconEnvironment): boolean {
  if (!apiBase || !slug) return false;
  if (env.webdriver || BOT_USER_AGENT.test(env.userAgent || "")) return false;
  const url = articleViewUrl(apiBase, slug);
  try {
    if (env.sendBeacon && env.sendBeacon(url, new Blob([""], { type: "text/plain" }))) return true;
  } catch {
    // Fall through to fetch.
  }
  if (!env.fetch) return false;
  env.fetch(url, { method: "POST", keepalive: true, mode: "no-cors", credentials: "omit" }).catch(() => {});
  return true;
}
