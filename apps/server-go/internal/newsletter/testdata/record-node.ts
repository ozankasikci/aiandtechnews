// Records the Node newsletter implementation's behavior as golden vectors for
// the Go port (internal/newsletter). Nothing here is inferred: every value in
// node-golden.json and node-case-mappings.json is produced by the real Node
// modules in apps/server/src/newsletter, better-sqlite3, and V8.
//
// No network, no listener, no real database: fetch and setTimeout are stubbed,
// databases are in-memory. Run from the repository root of a checkout whose
// apps/server dependencies are installed (NODE_SERVER_ROOT overrides the
// apps/server location, for example when running from a git worktree):
//
//   TZ=Europe/Istanbul apps/server/node_modules/.bin/tsx \
//     apps/server-go/internal/newsletter/testdata/record-node.ts \
//     apps/server-go/internal/newsletter/testdata
//
// TZ matters: V8 parses offset-less date-times (the dashboard's
// datetime-local values) in the process time zone, and the Go tests replay
// these vectors with Europe/Istanbul as the local zone.
import { createHmac } from "node:crypto";
import { writeFileSync } from "node:fs";
import path from "node:path";

type Json = null | boolean | number | string | Json[] | { [key: string]: Json };

const serverRoot = process.env.NODE_SERVER_ROOT || path.resolve(__dirname, "../../../../server");
const outputDir = process.argv[2] || __dirname;

// eslint-disable-next-line @typescript-eslint/no-require-imports
const tokens = require(path.join(serverRoot, "src/newsletter/tokens.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const email = require(path.join(serverRoot, "src/newsletter/email.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const readingTime = require(path.join(serverRoot, "src/newsletter/reading-time.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const serviceModule = require(path.join(serverRoot, "src/newsletter/service.ts"));
// eslint-disable-next-line @typescript-eslint/no-require-imports
const dbModule = require(path.join(serverRoot, "src/db.ts"));

const TEST_SECRET = "test-newsletter-token-secret-with-32-characters";
const CONTRACT_SECRET = "synthetic-contract-newsletter-secret-000000000000";
const UNICODE_SECRET = "ünïcödé-secret-🔐-with-more-than-thirty-two-characters";

function b64url(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function signed(payloadText: string, secret: string, encode: (value: string) => string = b64url): string {
  const payload = encode(payloadText);
  return `${payload}.${createHmac("sha256", secret).update(payload).digest("base64url")}`;
}

// ---------------------------------------------------------------- tokens
function recordTokens(): Json {
  const creates: Json[] = [];
  for (const secret of [TEST_SECRET, CONTRACT_SECRET, UNICODE_SECRET]) {
    const cases: [number, string, string | null][] = [
      [1, "unsubscribe", null],
      [42, "confirm", "2026-08-09T08:01:00.000Z"],
      [501, "confirm", "2026-09-21T12:00:00.000Z"],
      [502, "unsubscribe", "2026-09-21T12:00:00.000Z"],
      [504, "unsubscribe", null],
      [7, "unsubscribe", "2026-09-21T12:00:00.999Z"],
      [9007199254740991, "unsubscribe", null],
    ];
    for (const [id, purpose, expiresAt] of cases) {
      const token = tokens.createNewsletterToken(id, purpose, secret, expiresAt ? new Date(expiresAt) : undefined);
      creates.push({ secret, id, purpose, expiresAt, token });
    }
  }

  const now = Date.parse("2026-09-20T12:00:00.000Z");
  const valid = tokens.createNewsletterToken(5, "unsubscribe", TEST_SECRET);
  const [validPayload, validSignature] = valid.split(".");
  const tampered = `${validPayload}.${validSignature.slice(0, -1)}${validSignature.endsWith("A") ? "B" : "A"}`;
  const expiring = tokens.createNewsletterToken(9, "confirm", TEST_SECRET, new Date("2026-09-20T12:00:00.000Z"));
  const standardBase64 = (value: string) => Buffer.from(value, "utf8").toString("base64");
  const verifyCases: [string, string, string, number][] = [
    ["valid", valid, "unsubscribe", now],
    ["wrong purpose", valid, "confirm", now],
    ["trailing dot", `${valid}.`, "unsubscribe", now],
    ["empty third and fourth segment", `${valid}..x`, "unsubscribe", now],
    ["non-empty third segment", `${valid}.x`, "unsubscribe", now],
    ["payload only", `${validPayload}.`, "unsubscribe", now],
    ["signature only", `.${validSignature}`, "unsubscribe", now],
    ["no dot", validPayload, "unsubscribe", now],
    ["empty", "", "unsubscribe", now],
    ["tampered signature", tampered, "unsubscribe", now],
    ["signature with padding", `${valid}=`, "unsubscribe", now],
    ["other secret", tokens.createNewsletterToken(5, "unsubscribe", CONTRACT_SECRET), "unsubscribe", now],
    ["expires this second", expiring, "confirm", now],
    ["expires this second, 999ms later", expiring, "confirm", now + 999],
    ["expired one second ago", expiring, "confirm", now + 1000],
    ["v 2", signed('{"v":2,"id":5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["v string", signed('{"v":"1","id":5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["v 1.0", signed('{"v":1.0,"id":5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 0", signed('{"v":1,"id":0,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id -1", signed('{"v":1,"id":-1,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 1.5", signed('{"v":1,"id":1.5,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 5.0", signed('{"v":1,"id":5.0,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id 1e2", signed('{"v":1,"id":1e2,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id string", signed('{"v":1,"id":"5","purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id null", signed('{"v":1,"id":null,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["id missing", signed('{"v":1,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["duplicate id keeps last", signed('{"v":1,"id":5,"id":6,"purpose":"unsubscribe"}', TEST_SECRET), "unsubscribe", now],
    ["extra fields", signed('{"v":1,"id":5,"purpose":"unsubscribe","x":[1,{"y":null}]}', TEST_SECRET), "unsubscribe", now],
    ["exp null", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":null}', TEST_SECRET), "unsubscribe", now],
    ["exp string", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":"9999999999"}', TEST_SECRET), "unsubscribe", now],
    ["exp 1e400", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":1e400}', TEST_SECRET), "unsubscribe", now],
    ["exp fractional future", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":1789905600.5}', TEST_SECRET), "unsubscribe", now],
    ["exp fractional past", signed('{"v":1,"id":5,"purpose":"unsubscribe","exp":1789905599.5}', TEST_SECRET), "unsubscribe", now],
    ["not json", signed("not json", TEST_SECRET), "unsubscribe", now],
    ["json array", signed("[1]", TEST_SECRET), "unsubscribe", now],
    ["json null", signed("null", TEST_SECRET), "unsubscribe", now],
    ["json whitespace", signed(' \n{"v":1,"id":5,"purpose":"unsubscribe"}\t', TEST_SECRET), "unsubscribe", now],
    ["trailing json", signed('{"v":1,"id":5,"purpose":"unsubscribe"}{}', TEST_SECRET), "unsubscribe", now],
    ["standard base64 payload", signed('{"v":1,"id":5,"purpose":"unsubscribe","pad":"??>"}', TEST_SECRET, standardBase64), "unsubscribe", now],
    ["unicode secret", tokens.createNewsletterToken(5, "unsubscribe", UNICODE_SECRET), "unsubscribe", now],
  ];
  const verifies = verifyCases.map(([name, token, purpose, at]) => ({
    name,
    token,
    purpose,
    secret: name === "unicode secret" ? UNICODE_SECRET : TEST_SECRET,
    now: new Date(at).toISOString(),
    result: tokens.verifyNewsletterToken(token, purpose, name === "unicode secret" ? UNICODE_SECRET : TEST_SECRET, new Date(at)),
  }));
  return { creates, verifies };
}

// ---------------------------------------------------------------- reading time
function words(count: number, word = "word"): string {
  return Array.from({ length: count }, () => word).join(" ");
}

function recordReadingMinutes(): Json {
  const inputs: (string | null)[] = [
    null,
    "",
    "   ",
    "<p></p>",
    "word",
    words(109),
    words(110),
    words(329),
    words(330),
    words(440),
    `<p>${words(440)}</p>`,
    words(550),
    words(549),
    words(1000),
    `<p>${words(300)}</p><script>${words(300)}</script>`,
    `<SCRIPT type="x">${words(300)}</script>${words(300)}`,
    `<script>${words(300)}</style>${words(300)}</script>${words(30)}`,
    `<script>${words(300)}`,
    `<scriptx>${words(300)}</scriptx>`,
    `<style media="a>b">${words(300)}</STYLE>${words(10)}`,
    `<style>a</style><style>${words(400)}</style>b`,
    `<script>\n${words(300)}\n</script >${words(300)}`,
    "a&nbsp;b&NBSP;c",
    "a&amp;b &#8217; c &#x2019; d &Eacute;e",
    "a b c​d　e﻿",
    "<p>a</p><p>b</p>",
    "<a\nhref='x'>link</a> text",
    "<p>" + words(219) + "</p>",
    "  ",
    "&nbsp;",
    "<br/>",
    "a <!-- comment --> b",
    "1 < 2 and 3 > 2",
  ];
  return inputs.map((input) => ({ input, minutes: readingTime.readingMinutes(input) }));
}

// ---------------------------------------------------------------- emails
const siteUrl = "https://aiandtech.news";
const unsubscribeToken = tokens.createNewsletterToken(504, "unsubscribe", TEST_SECRET);
const unsubscribeUrl = `${siteUrl}/api/newsletter/unsubscribe?token=${encodeURIComponent(unsubscribeToken)}`;

const digestCases: { name: string; articles: Json[] }[] = [
  {
    name: "single",
    articles: [{ title: "A useful AI update", slug: "useful-ai-update", excerpt: "What changed and why it matters.", category: "AI", readingMinutes: 2 }],
  },
  {
    name: "one minute",
    articles: [{ title: "Short", slug: "short", excerpt: "Brief.", category: "AI", readingMinutes: 1 }],
  },
  {
    name: "grouped",
    articles: [
      { title: "First AI story", slug: "first-ai", excerpt: "One.", category: "AI", readingMinutes: 3 },
      { title: "A Go release", slug: "go-release", excerpt: "Two.", category: "Programming", readingMinutes: 1 },
      { title: "Second AI story", slug: "second-ai", excerpt: "Three.", category: "AI", readingMinutes: 4 },
      { title: "Funding round", slug: "funding", excerpt: "Four.", category: "Startups", readingMinutes: 2 },
      { title: "Another release", slug: "another", excerpt: "Five.", category: "Programming", readingMinutes: 5 },
    ],
  },
  {
    name: "escaping",
    articles: [
      {
        title: `<b>"Quotes" & 'apostrophes'</b>`,
        slug: "çok güzel/slug?x=1&y=ü 🚀(it's)*~!",
        excerpt: "<script>alert('x')</script> & more — em dash",
        category: "R&D <Labs> \"quoted\"",
        readingMinutes: 12,
      },
    ],
  },
  {
    name: "unicode categories",
    articles: [
      { title: "Straße", slug: "strasse", excerpt: "ß", category: "straße ﬁnance", readingMinutes: 1 },
      { title: "İstanbul", slug: "istanbul", excerpt: "i", category: "yapay zekâ ıi", readingMinutes: 1 },
    ],
  },
  { name: "empty", articles: [] },
];

function recordEmails(): Json {
  return {
    welcome: [
      { to: "reader@example.com", siteUrl, unsubscribeUrl, email: email.welcomeEmail("reader@example.com", siteUrl, unsubscribeUrl) },
      {
        to: "o'brien+news@example.com",
        siteUrl: "http://localhost:3000",
        unsubscribeUrl: "http://localhost:3000/api/newsletter/unsubscribe?token=a.b&x=<y>\"'",
        email: email.welcomeEmail("o'brien+news@example.com", "http://localhost:3000", "http://localhost:3000/api/newsletter/unsubscribe?token=a.b&x=<y>\"'"),
      },
    ],
    digests: digestCases.map(({ name, articles }) => ({
      name,
      to: "reader@example.com",
      siteUrl,
      unsubscribeUrl,
      articles,
      email: email.digestEmail("reader@example.com", articles, siteUrl, unsubscribeUrl),
    })),
    escapeHtml: ["", "plain", `&<>"'`, "a&amp;b", "üñí 🚀"].map((input) => ({ input, output: email.escapeHtml(input) })),
    encodeURIComponent: ["", "abc-_.!~*'()", "a b", "ç/?#&=+", "🚀", " ", "a%20b"].map((input) => ({
      input,
      output: encodeURIComponent(input),
    })),
  };
}

// ---------------------------------------------------------------- Resend sender
interface RecordedCall {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string;
}

interface ScriptedResponse {
  status: number;
  body?: string;
  retryAfter?: string;
  throws?: string;
}

async function runSender(
  environment: Record<string, string>,
  scripted: ScriptedResponse[],
  message: Json,
  idempotencyKey: string,
): Promise<Json> {
  const calls: RecordedCall[] = [];
  const delays: number[] = [];
  const originalFetch = globalThis.fetch;
  const originalSetTimeout = globalThis.setTimeout;
  let index = 0;
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    const headers: Record<string, string> = {};
    for (const [name, value] of Object.entries(init.headers as Record<string, string>)) headers[name] = value;
    calls.push({ url: String(url), method: String(init.method), headers, body: String(init.body) });
    const next = scripted[Math.min(index, scripted.length - 1)];
    index += 1;
    if (next.throws) throw new TypeError(next.throws);
    const responseHeaders: Record<string, string> = {};
    if (next.retryAfter !== undefined) responseHeaders["retry-after"] = next.retryAfter;
    return new Response(next.body ?? "", { status: next.status, headers: responseHeaders });
  }) as typeof fetch;
  globalThis.setTimeout = ((callback: () => void, delay: number) => {
    delays.push(delay);
    callback();
    return 0;
  }) as unknown as typeof setTimeout;
  try {
    const send = email.createResendSender(environment);
    const result = await send(message, idempotencyKey);
    return { calls: calls as unknown as Json, delays, result };
  } catch (error) {
    const err = error as Error;
    return {
      calls: calls as unknown as Json,
      delays,
      error: {
        name: err.constructor.name,
        configuration: err instanceof email.NewsletterConfigurationError,
        message: err.message,
      },
    };
  } finally {
    globalThis.fetch = originalFetch;
    globalThis.setTimeout = originalSetTimeout;
  }
}

async function recordSender(): Promise<Json> {
  const configured = {
    RESEND_API_KEY: " re_synthetic_key ",
    NEWSLETTER_FROM: " AI & Tech News <news@example.invalid> ",
    NEWSLETTER_REPLY_TO: " reply@example.invalid ",
  };
  const noReplyTo = { RESEND_API_KEY: "re_synthetic_key", NEWSLETTER_FROM: "News <news@example.invalid>" };
  const welcome = email.welcomeEmail("reader@example.com", siteUrl, unsubscribeUrl);
  const digest = email.digestEmail("reader@example.com", digestCases[3].articles, siteUrl, unsubscribeUrl);
  const bare = { to: "bare@example.com", subject: "Bare", html: "<p>x</p>", text: "x" };
  const ok = { status: 200, body: '{"id":"msg_1"}' };
  const cases: { name: string; env: Record<string, string>; scripted: ScriptedResponse[]; email: Json }[] = [
    { name: "success welcome", env: configured, scripted: [ok], email: welcome },
    { name: "success digest", env: configured, scripted: [ok], email: digest },
    { name: "no reply-to, no headers or tags", env: noReplyTo, scripted: [ok], email: bare },
    { name: "blank reply-to", env: { ...noReplyTo, NEWSLETTER_REPLY_TO: "   " }, scripted: [ok], email: bare },
    { name: "201 with id", env: noReplyTo, scripted: [{ status: 201, body: '{"id":"msg_2"}' }], email: bare },
    { name: "500 then success", env: noReplyTo, scripted: [{ status: 500, body: '{"message":"boom"}' }, ok], email: bare },
    {
      name: "429 retry-after then 503 then success",
      env: noReplyTo,
      scripted: [{ status: 429, retryAfter: "2", body: "{}" }, { status: 503, body: "oops" }, ok],
      email: bare,
    },
    { name: "500 three times", env: noReplyTo, scripted: [{ status: 500, body: '{"message":"boom"}' }], email: bare },
    { name: "502 three times without body", env: noReplyTo, scripted: [{ status: 502 }], email: bare },
    { name: "400 message", env: noReplyTo, scripted: [{ status: 400, body: '{"message":"Invalid from"}' }], email: bare },
    { name: "422 error field", env: noReplyTo, scripted: [{ status: 422, body: '{"error":"bad"}' }], email: bare },
    { name: "409 idempotency conflict", env: noReplyTo, scripted: [{ status: 409, body: '{"statusCode":409,"name":"invalid_idempotent_request","message":"Same idempotency key used with a different request payload."}' }], email: bare },
    { name: "401 not json", env: noReplyTo, scripted: [{ status: 401, body: "<html>no</html>" }], email: bare },
    { name: "200 without id", env: noReplyTo, scripted: [{ status: 200, body: "{}" }], email: bare },
    { name: "200 empty id", env: noReplyTo, scripted: [{ status: 200, body: '{"id":""}' }], email: bare },
    { name: "200 not json", env: noReplyTo, scripted: [{ status: 200, body: "ok" }], email: bare },
    { name: "message number", env: noReplyTo, scripted: [{ status: 400, body: '{"message":5}' }], email: bare },
    { name: "empty message falls back to error", env: noReplyTo, scripted: [{ status: 400, body: '{"message":"","error":"e"}' }], email: bare },
    { name: "error object", env: noReplyTo, scripted: [{ status: 400, body: '{"error":{"a":1}}' }], email: bare },
    { name: "json array body", env: noReplyTo, scripted: [{ status: 400, body: "[1]" }], email: bare },
    { name: "json string body", env: noReplyTo, scripted: [{ status: 400, body: '"text"' }], email: bare },
    { name: "json null body", env: noReplyTo, scripted: [{ status: 500, body: "null" }], email: bare },
    { name: "network error", env: noReplyTo, scripted: [{ status: 0, throws: "fetch failed" }], email: bare },
    { name: "missing key", env: { NEWSLETTER_FROM: "News <news@example.invalid>" }, scripted: [ok], email: bare },
    { name: "blank key", env: { ...noReplyTo, RESEND_API_KEY: "  " }, scripted: [ok], email: bare },
    { name: "missing from", env: { RESEND_API_KEY: "re_synthetic_key" }, scripted: [ok], email: bare },
  ];
  for (const retryAfter of ["0", "1.5", "abc", " 3 ", "0x2", "1e1", "-1", "Infinity", "", "Wed, 21 Oct 2026 07:28:00 GMT", "2 "]) {
    cases.push({
      name: `retry-after ${JSON.stringify(retryAfter)}`,
      env: noReplyTo,
      scripted: [{ status: 503, retryAfter, body: "{}" }, ok],
      email: bare,
    });
  }
  const results: Json[] = [];
  for (const testCase of cases) {
    results.push({
      name: testCase.name,
      env: testCase.env,
      scripted: testCase.scripted as unknown as Json,
      email: testCase.email,
      idempotencyKey: "newsletter-digest-2026-09-20-504",
      ...(await runSender(testCase.env, testCase.scripted, testCase.email, "newsletter-digest-2026-09-20-504") as Record<string, Json>),
    });
  }
  return results;
}

// ---------------------------------------------------------------- JavaScript primitives
function recordPrimitives(): Json {
  const dateInputs = [
    "2026-09-19 12:00:00",
    "2026-09-19T12:00:00.000Z",
    "2026-09-19T12:00:00Z",
    "2026-09-19T12:00:00",
    "2026-09-19T12:00",
    "2026-09-19T12:00:00.5",
    "2026-09-19T12:00:00.123456Z",
    "2026-09-19T12:00:00+03:00",
    "2026-09-19T12:00:00.000+05:30",
    "2026-09-19T12:00:00-0100",
    "2026-09-19t12:00:00z",
    "2026-09-19",
    "2026-09",
    "2026",
    "+002026-09-19T12:00:00.000Z",
    "2026-09-19 12:00",
    "2026-09-19 12:00:00.000",
    "2026-09-19 12:00:00Z",
    "2026-09-19T24:00:00Z",
    "2026-09-19T24:00:01Z",
    "2026-02-30T12:00:00Z",
    "2026-13-01T12:00:00Z",
    "2026-09-19T12:60:00Z",
    "2026-9-19",
    " 2026-09-19T12:00:00Z",
    "2026-09-19T12:00:00Z ",
    "Sat, 19 Sep 2026 12:00:00 GMT",
    "September 19, 2026",
    "19 Sep 2026 12:00",
    "not a date",
    "",
    "2026-09-00",
    "2026-00-10",
    "2026-09-19T12",
    "2026-09-19T1:00",
    "2026-09-19T12:00:00.",
    "2026-09-19T12:00+03",
    "2026-09-19T12:00:00+0300",
    "2026-09-19T12:00:00 +03:00",
    "2026-09-31T00:00:00Z",
    "-000001-01-01T00:00:00Z",
    "10000-01-01",
    "2026-09-19T12:00:00.000+25:00",
    "2026-09-19T12:00:00.000+23:59",
    "2026-09-19T12:00:00.000Z+",
    "2026-09-19T12:00:00.1234567890123Z",
    "2026-09-19T12:00:00,5Z",
    "2026-09-19 12:00:00+03:00",
    "2026-09-19T12:00:60Z",
    "2026-09-19T23:59:59.999Z",
    "2026-09-19T24:00",
    "2026-09-19T24:00:00.000Z",
    "2026-09-19T24:00:00.001Z",
    "20260919",
    "2026-09-19Z",
    "2026-09-19TZ",
    "2026-09-19T12:00:00Z0",
    "2026-09-19T12:00:00.000z",
    "2026-09-19t12:00",
  ];
  const datesParsed = dateInputs.map((input) => {
    const normalized = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(input) ? `${input.replace(" ", "T")}Z` : input;
    const value = Date.parse(normalized);
    return { input, ms: Number.isFinite(value) ? value : null };
  });

  const instants = [
    "2026-09-19T20:59:59.999Z",
    "2026-09-19T21:00:00.000Z",
    "2026-09-20T05:00:00.000Z",
    "2026-12-31T21:00:00.000Z",
    "2015-03-29T00:30:00.000Z",
    "2015-10-25T01:30:00.000Z",
    "2016-09-07T20:59:59.000Z",
    "2016-09-07T21:00:00.000Z",
  ];
  const istanbulDates = instants.map((instant) => {
    const parts = new Intl.DateTimeFormat("en-CA", { timeZone: "Europe/Istanbul", year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(new Date(instant));
    const get = (type: string) => parts.find((part) => part.type === type)?.value || "";
    return { instant, edition: `${get("year")}-${get("month")}-${get("day")}` };
  });

  const parseIntInputs = ["2", " 7", "7abc", "-3", "0", "", "abc", "1e3", "0x10", "+5", "  +5", "99999999999999999999", "12.9", " 5", "﻿8", "- 5", "--5", "30,40", "[object Object]", "Infinity"];
  const parseInts = parseIntInputs.map((input) => {
    const value = parseInt(input, 10);
    return { input, value: Number.isFinite(value) ? value : null };
  });

  const emailInputs = [
    " Reader@Example.com ",
    "a@b.c",
    "a@b",
    "@b.c",
    "a@.c",
    "a@b.",
    "a@b..c",
    "a b@c.d",
    "a@b.c\n",
    " a@b.c ",
    "a@b.c@d.e",
    "İSTANBUL@ÖRNEK.COM",
    "ΑΣ@ΣΑΣ.GR",
    "ΣΑ.Σ@X.GR",
    "a@b c.d",
    "a@b​c.d",
    `${"a".repeat(250)}@b.c`,
    `${"a".repeat(249)}@b.c`,
    `${"🚀".repeat(125)}@b.c`,
    `${"🚀".repeat(124)}@b.c`,
    "",
    "   ",
    "STRASSE@ß.DE",
  ];

  const truncations = ["", "short", "x".repeat(81), `${"x".repeat(79)}🚀tail`, `${"x".repeat(78)}🚀tail`].map((input) => ({
    input,
    slice80: input.slice(0, 80),
  }));

  const stringifyInputs = ["", "plain", 'a"b\\c/d', "\b\f\n\r\t\v", "\u0000\u0001\u001f\u007f\u0080", "\u2028\u2029", "<>&'", "é🚀\ufeff"];
  const stringified = stringifyInputs.map((input) => ({ input, output: JSON.stringify(input) }));

  return { datesParsed, istanbulDates, parseInts, emailInputs, truncations, stringified };
}

// Every code point whose JavaScript toUpperCase/toLowerCase differs from itself.
function recordCaseMappings(): Json {
  const upper: [number, string][] = [];
  const lower: [number, string][] = [];
  for (let codePoint = 0; codePoint <= 0x10ffff; codePoint += 1) {
    if (codePoint >= 0xd800 && codePoint <= 0xdfff) continue;
    const value = String.fromCodePoint(codePoint);
    const upperValue = value.toUpperCase();
    const lowerValue = value.toLowerCase();
    if (upperValue !== value) upper.push([codePoint, upperValue]);
    if (lowerValue !== value) lower.push([codePoint, lowerValue]);
  }
  const contextual = ["ΑΣ", "Σ", "ΑΣ Α", "ΑΣΑ", "Α'Σ", "ΑΣ'", "ΑΣ.Β", "1Σ", "ΑΣ1", "ΆΣ", "ΑΣ́", "ΑΣ́Α"].map((input) => ({
    input,
    lower: input.toLowerCase(),
  }));
  return { unicodeVersion: process.versions.unicode, upper, lower, contextual };
}

// ---------------------------------------------------------------- service scenarios
function openDatabase() {
  const db = dbModule.openDatabase(":memory:");
  dbModule.initializeDatabase(db, { seedDefaults: false });
  return db;
}

function stubTimers() {
  const delays: number[] = [];
  const original = globalThis.setTimeout;
  globalThis.setTimeout = ((callback: () => void, delay: number) => {
    delays.push(delay);
    callback();
    return 0;
  }) as unknown as typeof setTimeout;
  return { delays, restore: () => { globalThis.setTimeout = original; } };
}

function rows(db: any, sql: string): Json {
  return db.prepare(sql).all() as Json;
}

function schemaOf(db: any): Json {
  return db
    .prepare("SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE name IN ('subscribers','newsletter_deliveries','newsletter_editions','idx_subscribers_status','idx_newsletter_deliveries_edition') ORDER BY name")
    .all() as Json;
}

async function recordDigestSelection(): Promise<Json> {
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Programming', 'programming')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  const now = new Date("2026-09-20T05:00:00.000Z");
  // [slug, status, published_at, category, words]
  const articles: [string, string, string | null, number, number][] = [
    ["future-iso", "published", "2026-09-20T05:00:00.001Z", 1, 10],
    ["exactly-now", "published", "2026-09-20T05:00:00.000Z", 1, 220],
    ["sqlite-recent", "published", "2026-09-20 04:00:00", 2, 330],
    ["local-datetime", "published", "2026-09-20T07:30", 1, 500],
    ["cutoff-exact", "published", "2026-09-18T23:00:00.000Z", 2, 1],
    ["cutoff-minus-1ms", "published", "2026-09-18T22:59:59.999Z", 1, 1],
    ["draft-recent", "draft", "2026-09-20T04:00:00.000Z", 1, 1],
    ["scheduled-recent", "scheduled", "2026-09-20T04:00:00.000Z", 1, 1],
    ["null-date", "published", null, 1, 1],
    ["garbage-date", "published", "zzz", 1, 1],
    ["sqlite-old", "published", "2026-09-18 01:00:00", 1, 1],
    ["offset", "published", "2026-09-20T06:00:00+03:00", 2, 900],
    ["date-only", "published", "2026-09-20", 1, 50],
  ];
  const insert = db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES (?, ?, ?, ?, ?, 1, ?, ?)`);
  for (const [slug, status, publishedAt, category, count] of articles) {
    insert.run(`Title ${slug}`, slug, `Excerpt ${slug}`, `<p>${words(count)}</p>`, category, status, publishedAt);
  }
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'one@example.com', 'active', 'x', 'x')").run();
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ to: message.to, subject: message.subject, idempotencyKey, html: message.html, text: message.text, headers: message.headers, tags: message.tags });
      return { id: `message-${sent.length}` };
    },
  );
  const timers = stubTimers();
  try {
    const result = await service.sendDailyDigest(now);
    return {
      now: now.toISOString(),
      articles: articles as unknown as Json,
      result,
      editions: rows(db, "SELECT id, edition_key, subject, articles, created_at FROM newsletter_editions"),
      deliveries: rows(db, "SELECT id, subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries"),
      sent,
      delays: timers.delays,
    };
  } finally {
    timers.restore();
    db.close();
  }
}

async function recordDigestWindow(): Promise<Json> {
  // Twenty published articles whose published_at text sorts above the recent
  // ones push the recent ones out of the SQL LIMIT 20 window.
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  const insert = db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES (?, ?, ?, '', 1, 1, 'published', ?)`);
  for (let index = 0; index < 20; index += 1) insert.run(`Future ${index}`, `future-${index}`, "x", `2027-01-${String(index + 1).padStart(2, "0")}T00:00:00.000Z`);
  insert.run("Recent", "recent", "x", "2026-09-20T04:00:00.000Z");
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET }, async () => ({ id: "unused" }));
  try {
    const result = await service.sendDailyDigest(new Date("2026-09-20T05:00:00.000Z"));
    return { result, editions: rows(db, "SELECT edition_key FROM newsletter_editions") };
  } finally {
    db.close();
  }
}

async function recordDigestTopFive(): Promise<Json> {
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Code', 'code')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  const insert = db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES (?, ?, ?, ?, ?, 1, 'published', ?)`);
  const stamps = [
    "2026-09-20 04:59:00",
    "2026-09-20T04:00:00.000Z",
    "2026-09-20 03:00:00",
    "2026-09-20T02:00:00.000Z",
    "2026-09-20 01:00:00",
    "2026-09-20T00:30:00.000Z",
    "2026-09-19 23:00:00",
  ];
  stamps.forEach((stamp, index) => insert.run(`Story ${index}`, `story-${index}`, `Excerpt ${index}`, `<p>${words(index * 150)}</p>`, (index % 2) + 1, stamp));
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news/some/path?q=1", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ to: message.to, subject: message.subject, idempotencyKey, html: message.html, text: message.text });
      return { id: `message-${sent.length}` };
    },
  );
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (3, 'three@example.com', 'active', 'x', 'x')").run();
  const timers = stubTimers();
  try {
    const result = await service.sendDailyDigest(new Date("2026-09-20T05:00:00.000Z"));
    return { stamps, result, editions: rows(db, "SELECT edition_key, subject, articles, created_at FROM newsletter_editions"), sent };
  } finally {
    timers.restore();
    db.close();
  }
}

async function recordDeliveryLifecycle(): Promise<Json> {
  const db = openDatabase();
  db.prepare("INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')").run();
  db.prepare("INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')").run();
  db.prepare(`INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at)
    VALUES ('Lead story', 'lead', 'Lead excerpt', '<p>lead</p>', 1, 1, 'published', '2026-09-20 04:00:00')`).run();
  const subscribers: [number, string, string][] = [
    [1, "one@example.com", "active"],
    [2, "pending@example.com", "pending"],
    [3, "fails@example.com", "active"],
    [4, "gone@example.com", "unsubscribed"],
    [5, "stuck@example.com", "active"],
    [6, "five@example.com", "active"],
  ];
  const insertSubscriber = db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (?, ?, ?, 'x', 'x')");
  for (const subscriber of subscribers) insertSubscriber.run(...subscriber);
  // A delivery left 'sending' by a crashed run, and one already sent.
  db.prepare(`INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, error, created_at)
    VALUES (5, '2026-09-20', 'sending', 'old-id', 'old error', '2026-09-20T04:59:00.000Z')`).run();
  db.prepare(`INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, created_at, sent_at)
    VALUES (6, '2026-09-20', 'sent', 'sent-id', '2026-09-20T04:59:00.000Z', '2026-09-20T04:59:30.000Z')`).run();
  let failFor: string | null = "fails@example.com";
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ to: message.to, idempotencyKey });
      if (message.to === failFor) throw new Error(`${"é".repeat(499)}🚀 provider rejected`);
      return { id: `message-${sent.length}` };
    },
  );
  const timers = stubTimers();
  try {
    const first = await service.sendDailyDigest(new Date("2026-09-20T05:00:00.000Z"));
    const afterFirst = rows(db, "SELECT subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries ORDER BY subscriber_id");
    const firstDelays = [...timers.delays];
    timers.delays.length = 0;
    failFor = null;
    const second = await service.sendDailyDigest(new Date("2026-09-20T05:10:00.000Z"));
    const afterSecond = rows(db, "SELECT subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries ORDER BY subscriber_id");
    return {
      subscribers: subscribers as unknown as Json,
      first,
      afterFirst,
      firstDelays,
      second,
      afterSecond,
      secondDelays: timers.delays,
      sent,
      editions: rows(db, "SELECT edition_key, subject, articles, created_at FROM newsletter_editions"),
    };
  } finally {
    timers.restore();
    db.close();
  }
}

async function recordSubscriptions(): Promise<Json> {
  const db = openDatabase();
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, async () => {
    throw new Error("signup must not send");
  });
  const now = new Date("2026-08-09T08:00:00.000Z");
  const later = new Date("2026-08-09T09:00:00.000Z");
  db.prepare("INSERT INTO subscribers (id, email, status, confirmed_at, created_at, updated_at) VALUES (10, 'pending@example.com', 'pending', NULL, 'c', 'u')").run();
  db.prepare("INSERT INTO subscribers (id, email, status, source_placement, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (11, 'gone@example.com', 'unsubscribed', 'old', '2026-01-01T00:00:00.000Z', '2026-02-01T00:00:00.000Z', 'c', 'u')").run();
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (12, 'MixedCase@Example.com', 'active', 'c', 'u')").run();
  const steps: [string, string, Date][] = [
    [" Reader@Example.com ", "inline", now],
    ["reader@example.com", "header", later],
    ["PENDING@example.com", "footer", later],
    ["gone@example.com", "x".repeat(79) + "🚀tail", later],
    ["mixedcase@example.com", "archive_index", later],
    ["not-an-email", "inline", later],
  ];
  const outcomes: Json[] = [];
  for (const [address, placement, at] of steps) {
    try {
      outcomes.push({ address, placement, at: at.toISOString(), result: await service.requestSubscription(address, placement, at) });
    } catch (error) {
      outcomes.push({ address, placement, at: at.toISOString(), error: { name: (error as Error).constructor.name, message: (error as Error).message } });
    }
  }
  const normalized: Json[] = [];
  const probe = openDatabase();
  const probeService = new serviceModule.NewsletterService(probe, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, async () => ({ id: "x" }));
  for (const input of (recordPrimitives() as any).emailInputs as string[]) {
    try {
      await probeService.requestSubscription(input, "probe", now);
      const stored = probe.prepare("SELECT email FROM subscribers ORDER BY id DESC LIMIT 1").get() as { email: string };
      normalized.push({ input, email: stored.email });
      probe.prepare("DELETE FROM subscribers").run();
    } catch (error) {
      normalized.push({ input, error: (error as Error).message });
    }
  }
  probe.close();
  try {
    return {
      outcomes,
      subscribers: rows(db, "SELECT id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at FROM subscribers ORDER BY id"),
      normalized,
    };
  } finally {
    db.close();
  }
}

async function recordConfirmAndUnsubscribe(): Promise<Json> {
  const db = openDatabase();
  const insert = db.prepare("INSERT INTO subscribers (id, email, status, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'c', 'u')");
  insert.run(1, "pending@example.com", "pending", null, null);
  insert.run(2, "active@example.com", "active", "2026-01-01T00:00:00.000Z", null);
  insert.run(3, "gone@example.com", "unsubscribed", "2026-01-01T00:00:00.000Z", "2026-02-01T00:00:00.000Z");
  insert.run(4, "pending-fail@example.com", "pending", null, null);
  const sent: Json[] = [];
  const service = new serviceModule.NewsletterService(
    db,
    { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: TEST_SECRET },
    async (message: any, idempotencyKey: string) => {
      sent.push({ ...message, idempotencyKey });
      if (message.to === "pending-fail@example.com") throw new Error("provider down");
      return { id: "welcome-id" };
    },
  );
  const now = new Date("2026-09-20T12:00:00.000Z");
  const expiry = new Date("2026-09-21T12:00:00.000Z");
  const confirm = (id: number) => tokens.createNewsletterToken(id, "confirm", TEST_SECRET, expiry);
  const unsubscribe = (id: number) => tokens.createNewsletterToken(id, "unsubscribe", TEST_SECRET);
  const originalError = console.error;
  console.error = () => {};
  try {
    const confirms: Json[] = [];
    for (const [name, token] of [
      ["pending", confirm(1)],
      ["pending again", confirm(1)],
      ["active", confirm(2)],
      ["unsubscribed", confirm(3)],
      ["unknown", confirm(99)],
      ["unsubscribe token", unsubscribe(1)],
      ["garbage", "garbage"],
      ["send fails", confirm(4)],
    ] as [string, string][]) {
      confirms.push({ name, token, result: await service.confirmSubscription(token, now) });
    }
    const unsubscribes: Json[] = [];
    for (const [name, token] of [
      ["active", unsubscribe(2)],
      ["again", unsubscribe(2)],
      ["pending", unsubscribe(4)],
      ["unknown", unsubscribe(99)],
      ["confirm token", confirm(1)],
      ["garbage", "a.b"],
    ] as [string, string][]) {
      unsubscribes.push({ name, token, result: service.unsubscribe(token, now) });
    }
    return {
      now: now.toISOString(),
      confirms,
      unsubscribes,
      sent,
      subscribers: rows(db, "SELECT id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at FROM subscribers ORDER BY id"),
    };
  } finally {
    console.error = originalError;
    db.close();
  }
}

function recordEditions(): Json {
  const db = openDatabase();
  const insert = db.prepare("INSERT INTO newsletter_editions (edition_key, subject, articles, created_at) VALUES (?, ?, ?, ?)");
  insert.run("2026-09-17", "Object", '{"not":"array"}', "2026-09-17T05:00:00.000Z");
  insert.run("2026-09-18", "Malformed", "{malformed", "2026-09-18T05:00:00.000Z");
  insert.run("2026-09-19", "Loose", ' [ {"title":"T","extra":[1,2],"readingMinutes":1.5}, 7, null ] ', "2026-09-19T05:00:00.000Z");
  insert.run("2026-09-20", "Typed", JSON.stringify([{ title: "<T&>", slug: "s", excerpt: "e", category: "c", readingMinutes: 2 }]), "2026-09-20T05:00:00.000Z");
  insert.run("2026-09-16T", "Odd key", "[]", "x");
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news" }, async () => ({ id: "x" }));
  try {
    const lists: Json[] = [];
    for (const limit of [30, 2, 0, -5, 1, 1000, 100]) lists.push({ limit, editions: service.listEditions(limit) });
    const gets: Json[] = [];
    for (const key of ["2026-09-19", "2026-09-17", "2026-09-18", "missing", "2026-09-16T"]) gets.push({ key, edition: service.getEdition(key) });
    return { lists, gets, jsonText: JSON.stringify(service.getEdition("2026-09-19")) };
  } finally {
    db.close();
  }
}

function recordSiteUrls(): Json {
  const inputs = [undefined, "", "   ", "https://aiandtech.news", "https://aiandtech.news/", "https://AIANDTECH.news:443/path?q#h", "https://example.com:8443/x",
    "http://localhost:3000", "http://localhost", "http://example.com", "ftp://localhost", "not a url", " https://aiandtech.news ", "https://user:pw@example.com/"];
  return inputs.map((input) => {
    try {
      const service = new serviceModule.NewsletterService(openDatabase(), { NEWSLETTER_SITE_URL: input }, async () => ({ id: "x" }));
      return { input: input ?? null, siteUrl: (service as any).siteUrl };
    } catch (error) {
      return { input: input ?? null, error: { name: (error as Error).constructor.name, message: (error as Error).message, configuration: error instanceof email.NewsletterConfigurationError } };
    }
  });
}

function recordTokenSecrets(): Json {
  const db = openDatabase();
  const results: Json[] = [];
  for (const secret of [undefined, "", "short", "x".repeat(31), "x".repeat(32), `  ${"x".repeat(31)}  `, `  ${"x".repeat(32)}  `]) {
    const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: secret }, async () => ({ id: "x" }));
    try {
      results.push({ secret: secret ?? null, unsubscribe: service.unsubscribe("a.b") });
    } catch (error) {
      results.push({ secret: secret ?? null, error: { message: (error as Error).message, configuration: error instanceof email.NewsletterConfigurationError } });
    }
  }
  // The trimmed secret is the HMAC key.
  const padded = `  ${TEST_SECRET}  `;
  const service = new serviceModule.NewsletterService(db, { NEWSLETTER_SITE_URL: "https://aiandtech.news", NEWSLETTER_TOKEN_SECRET: padded }, async () => ({ id: "x" }));
  db.prepare("INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'a@b.c', 'active', 'c', 'u')").run();
  const trimmedKeyResult = service.unsubscribe(tokens.createNewsletterToken(1, "unsubscribe", TEST_SECRET), new Date("2026-09-20T12:00:00.000Z"));
  db.close();
  return { results, trimmedKeyResult };
}

async function main() {
  const schemaDb = openDatabase();
  const golden = {
    note: "Generated by record-node.ts from the Node implementation. Do not edit by hand.",
    node: process.version,
    timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    schema: schemaOf(schemaDb),
    tokens: recordTokens(),
    readingMinutes: recordReadingMinutes(),
    emails: recordEmails(),
    sender: await recordSender(),
    primitives: recordPrimitives(),
    digestSelection: await recordDigestSelection(),
    digestWindow: await recordDigestWindow(),
    digestTopFive: await recordDigestTopFive(),
    deliveryLifecycle: await recordDeliveryLifecycle(),
    subscriptions: await recordSubscriptions(),
    confirmAndUnsubscribe: await recordConfirmAndUnsubscribe(),
    editions: recordEditions(),
    siteUrls: recordSiteUrls(),
    tokenSecrets: recordTokenSecrets(),
  };
  schemaDb.close();
  writeFileSync(path.join(outputDir, "node-golden.json"), `${JSON.stringify(golden, null, 1)}\n`);
  writeFileSync(path.join(outputDir, "node-case-mappings.json"), `${JSON.stringify(recordCaseMappings())}\n`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
