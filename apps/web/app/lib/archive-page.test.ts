import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  ARCHIVE_PAGE_SIZE,
  archiveHref,
  getArchivePage,
  parseArchivePage,
} from "../archive/archive-page";
import type { ApiArticle, getArticles } from "./api";

function articles(count: number): ApiArticle[] {
  return Array.from({ length: count }, (_, index) => ({
    id: index + 1,
    slug: `story-${index + 1}`,
  })) as ApiArticle[];
}

test("archive page query accepts only positive, safe, unambiguous integers", () => {
  assert.equal(parseArchivePage(undefined), 1);
  assert.equal(parseArchivePage("1"), 1);
  assert.equal(parseArchivePage("17"), 17);

  for (const invalid of ["", "0", "-1", "01", "1.5", "1e3", "9007199254740992", ["1", "2"]]) {
    assert.equal(parseArchivePage(invalid), null);
  }
});

test("archive URLs use the clean first page and distinct URLs for later pages", () => {
  assert.equal(archiveHref(1), "/archive");
  assert.equal(archiveHref(2), "/archive?page=2");
  assert.equal(archiveHref(17), "/archive?page=17");
});

test("archive fetches a complete page and a partial final page", async () => {
  const requested: Array<{ page?: number; limit?: number }> = [];
  const getPage: typeof getArticles = async (opts) => {
    requested.push(opts || {});
    return {
      page: opts?.page || 1,
      total: 50,
      totalPages: 3,
      articles: articles(opts?.page === 3 ? 2 : ARCHIVE_PAGE_SIZE),
    };
  };

  const first = await getArchivePage(undefined, getPage);
  const last = await getArchivePage("3", getPage);

  assert.equal(first.state, "found");
  if (first.state === "found") assert.equal(first.articles.length, ARCHIVE_PAGE_SIZE);
  assert.equal(last.state, "found");
  if (last.state === "found") assert.equal(last.articles.length, 2);
  assert.deepEqual(requested, [
    { page: 1, limit: ARCHIVE_PAGE_SIZE },
    { page: 3, limit: ARCHIVE_PAGE_SIZE },
  ]);
});

test("archive treats invalid and out-of-range pages as missing", async () => {
  let calls = 0;
  const getPage: typeof getArticles = async (opts) => {
    calls++;
    return { page: opts?.page || 1, total: 24, totalPages: 1, articles: [] };
  };

  assert.deepEqual(await getArchivePage("nope", getPage), { state: "missing" });
  assert.equal(calls, 0);
  assert.deepEqual(await getArchivePage("2", getPage), { state: "missing" });
  assert.equal(calls, 1);
});

test("empty archive has a valid first page and no second page", async () => {
  const getPage: typeof getArticles = async (opts) => ({
    page: opts?.page || 1,
    total: 0,
    totalPages: 0,
    articles: [],
  });

  assert.equal((await getArchivePage(undefined, getPage)).state, "found");
  assert.equal((await getArchivePage("2", getPage)).state, "missing");
});

test("API failures and incomplete responses are unavailable, not missing", async () => {
  const unavailable: typeof getArticles = async () => null;
  const broken: typeof getArticles = async () => { throw new Error("offline"); };
  const partial: typeof getArticles = async () => ({
    page: 1,
    total: 24,
    totalPages: 1,
    articles: articles(1),
  });

  assert.equal((await getArchivePage(undefined, unavailable)).state, "unavailable");
  assert.equal((await getArchivePage(undefined, broken)).state, "unavailable");
  assert.equal((await getArchivePage(undefined, partial)).state, "unavailable");
});

test("archive and homepage expose crawlable article and pagination links", () => {
  const archiveSource = readFileSync(new URL("../archive/page.tsx", import.meta.url), "utf8");
  const homeSource = readFileSync(new URL("../page.tsx", import.meta.url), "utf8");

  assert.match(archiveSource, /<Link href=\{`\/article\/\$\{article\.slug\}`\}/);
  assert.match(archiveSource, /href=\{archiveHref\(page - 1\)\}/);
  assert.match(archiveSource, /href=\{archiveHref\(page \+ 1\)\}/);
  assert.match(archiveSource, /href=\{archiveHref\(1\)\}/);
  assert.match(archiveSource, /alternates: \{ canonical: canonicalUrl \}/);
  assert.match(homeSource, /<Link href="\/archive"/);
});
