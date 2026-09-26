import assert from "node:assert/strict";
import test from "node:test";
import { readVisited, recordVisit } from "./visited";

function fakeStorage() {
  const data = new Map<string, string>();
  return {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => void data.set(key, value),
  };
}

test("visits are remembered newest first, without repeats, up to 50", () => {
  const storage = fakeStorage();
  recordVisit("a", storage);
  recordVisit("b", storage);
  recordVisit("a", storage);
  assert.deepEqual(readVisited(storage), ["a", "b"]);
  for (let i = 0; i < 60; i++) recordVisit(`s${i}`, storage);
  assert.equal(readVisited(storage).length, 50);
  assert.equal(readVisited(storage)[0], "s59");
});

test("unavailable or corrupt storage reads as no visits and never throws", () => {
  const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
  assert.deepEqual(readVisited(broken), []);
  assert.doesNotThrow(() => recordVisit("a", broken));
  const corrupt = { getItem: () => "{not json", setItem: () => {} };
  assert.deepEqual(readVisited(corrupt), []);
  assert.deepEqual(readVisited(undefined), []);
});
