// The reader's quiz history, kept only in this browser: finished quizzes by
// UTC day, for the streak and for showing today's result again on return.
// Storage may be missing or blocked, so every access fails soft.

type Storage = Pick<globalThis.Storage, "getItem" | "setItem">;

export interface QuizResult {
  day: string;
  number: number;
  answers: number[];
  correct: boolean[];
}

export interface QuizProgress {
  results: Record<string, QuizResult>;
}

const KEY = "aitn.quiz";
const KEEP_DAYS = 60;

function browserStorage(): Storage | undefined {
  try {
    return typeof window === "undefined" ? undefined : window.localStorage;
  } catch {
    return undefined;
  }
}

export function loadProgress(storage: Storage | undefined = browserStorage()): QuizProgress {
  try {
    const value = JSON.parse(storage?.getItem(KEY) ?? "{}");
    return value && typeof value.results === "object" && value.results ? { results: value.results } : { results: {} };
  } catch {
    return { results: {} };
  }
}

export function recordResult(result: QuizResult, storage: Storage | undefined = browserStorage()): void {
  try {
    const progress = loadProgress(storage);
    progress.results[result.day] = result;
    const days = Object.keys(progress.results).sort().slice(-KEEP_DAYS);
    storage?.setItem(KEY, JSON.stringify({ results: Object.fromEntries(days.map((d) => [d, progress.results[d]])) }));
  } catch {
    // Storage full or blocked: the result just isn't remembered.
  }
}

function previousDay(day: string): string {
  const date = new Date(`${day}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() - 1);
  return date.toISOString().slice(0, 10);
}

// Consecutive days played up to `today`. A streak survives until the end of
// the day after the last quiz, so it shows before today's quiz is played.
export function streakOn(progress: QuizProgress, today: string): number {
  let day = progress.results[today] ? today : previousDay(today);
  let streak = 0;
  while (progress.results[day]) {
    streak++;
    day = previousDay(day);
  }
  return streak;
}

export function shareText(result: QuizResult): string {
  const score = result.correct.filter(Boolean).length;
  const squares = result.correct.map((ok) => (ok ? "🟩" : "🟥")).join("");
  return `AI & Tech News daily quiz No. ${result.number}: ${score}/${result.correct.length}\n${squares}\nhttps://www.aiandtech.news/quiz`;
}
