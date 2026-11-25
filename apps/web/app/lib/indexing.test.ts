import assert from "node:assert/strict";
import test from "node:test";
import { MIN_INDEXED_WORDS, articleWordCount, isIndexable } from "./indexing";

const words = (n: number) => Array.from({ length: n }, (_, i) => `word${i}`).join(" ");

test("counts words in article HTML, ignoring tags", () => {
  assert.equal(articleWordCount("<p>One two</p><h2>Three</h2><p>four&nbsp;five</p>"), 5);
  assert.equal(articleWordCount(""), 0);
});

test("articles under the minimum are kept out of search indexes", () => {
  assert.equal(isIndexable({ content: `<p>${words(MIN_INDEXED_WORDS - 1)}</p>` }), false);
  assert.equal(isIndexable({ content: `<p>${words(MIN_INDEXED_WORDS)}</p>` }), true);
  assert.equal(isIndexable({ content: undefined }), false);
});
