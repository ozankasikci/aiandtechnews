// Articles this browser has opened, newest first, so read-next suggestions can
// skip them. Kept in localStorage only; storage may be missing or blocked, so
// every access fails soft.

type Storage = Pick<globalThis.Storage, "getItem" | "setItem">;

const KEY = "aitn.visited";
const LIMIT = 50;

function browserStorage(): Storage | undefined {
  try {
    return typeof window === "undefined" ? undefined : window.localStorage;
  } catch {
    return undefined;
  }
}

export function readVisited(storage: Storage | undefined = browserStorage()): string[] {
  try {
    const value = JSON.parse(storage?.getItem(KEY) ?? "[]");
    return Array.isArray(value) ? value.filter((slug): slug is string => typeof slug === "string") : [];
  } catch {
    return [];
  }
}

export function recordVisit(slug: string, storage: Storage | undefined = browserStorage()): void {
  try {
    const visited = [slug, ...readVisited(storage).filter((s) => s !== slug)].slice(0, LIMIT);
    storage?.setItem(KEY, JSON.stringify(visited));
  } catch {
    // Storage full or blocked: suggestions just don't skip visited stories.
  }
}
