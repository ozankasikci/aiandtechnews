import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { loadProgress, recordResult, shareText, streakOn } from "./quiz-progress";

function fakeStorage() {
  const data = new Map<string, string>();
  return { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) };
}

test("a finished quiz is remembered by day with the answers given", () => {
  const storage = fakeStorage();
  recordResult({ day: "2026-09-27", number: 3, answers: [1, 0, 2], correct: [true, false, true] }, storage);
  const progress = loadProgress(storage);
  assert.deepEqual(progress.results["2026-09-27"], { day: "2026-09-27", number: 3, answers: [1, 0, 2], correct: [true, false, true] });
});

test("the streak counts consecutive days played, ending today or yesterday", () => {
  const storage = fakeStorage();
  for (const day of ["2026-09-24", "2026-09-25", "2026-09-26"]) {
    recordResult({ day, number: 1, answers: [0, 0, 0], correct: [true, true, true] }, storage);
  }
  const progress = loadProgress(storage);
  assert.equal(streakOn(progress, "2026-09-26"), 3);
  assert.equal(streakOn(progress, "2026-09-27"), 3, "still alive the next day before playing");
  assert.equal(streakOn(progress, "2026-09-28"), 0, "a missed day ends it");
  recordResult({ day: "2026-09-27", number: 2, answers: [0, 0, 0], correct: [true, true, true] }, storage);
  assert.equal(streakOn(loadProgress(storage), "2026-09-27"), 4);
});

test("the share text shows the score as squares without giving away answers", () => {
  const text = shareText({ day: "2026-09-27", number: 12, answers: [1, 0, 2], correct: [true, false, true] });
  assert.equal(text, "AI & Tech News daily quiz No. 12: 2/3\n🟩🟥🟩\nhttps://www.aiandtech.news/quiz");
});

test("broken or missing storage never throws and reads as no progress", () => {
  const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
  assert.deepEqual(loadProgress(broken).results, {});
  assert.doesNotThrow(() => recordResult({ day: "d", number: 1, answers: [], correct: [] }, broken));
  assert.deepEqual(loadProgress({ getItem: () => "nonsense", setItem: () => {} }).results, {});
});

test("the quiz page, sidebar card, footer and sitemap are wired up", () => {
  const page = readFileSync(new URL("../quiz/page.tsx", import.meta.url), "utf8");
  const sidebar = readFileSync(new URL("../components/Sidebar.tsx", import.meta.url), "utf8");
  const footer = readFileSync(new URL("../components/Footer.tsx", import.meta.url), "utf8");
  const sitemap = readFileSync(new URL("../sitemap.ts", import.meta.url), "utf8");
  const api = readFileSync(new URL("./api.ts", import.meta.url), "utf8");
  assert.match(api, /apiFetch<\{ quiz: ApiQuiz \}>\("\/api\/quiz\/today", undefined, cache\)/);
  assert.match(page, /getTodayQuiz\(\)/);
  assert.match(page, /<QuizGame /);
  assert.match(sidebar, /<QuizCard /);
  assert.match(footer, /href: "\/quiz"/);
  assert.match(sitemap, /\/quiz`/);
});
