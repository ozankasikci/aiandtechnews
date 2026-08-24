import assert from "node:assert/strict";
import test from "node:test";
import { countWords, readingTimeLabel, readingTimeMinutes } from "./reading-time";

test("markup and entities do not count as words", () => {
  const html = '<p class="a-very-long-class-name-here">Two words</p>';
  assert.equal(countWords(html), 2);
  assert.equal(countWords("<p>a&nbsp;b</p>"), 2);
  assert.equal(countWords("<style>.x{color:red}</style><p>only this</p>"), 2);
});

test("empty and missing bodies fall back to the minimum", () => {
  assert.equal(readingTimeMinutes(""), 1);
  assert.equal(readingTimeMinutes(null), 1);
  assert.equal(readingTimeMinutes("<p></p>"), 1);
});

test("reading time scales with word count, not character count", () => {
  const words = (n: number) => `<p>${Array.from({ length: n }, () => "word").join(" ")}</p>`;
  assert.equal(readingTimeMinutes(words(220)), 1);
  assert.equal(readingTimeMinutes(words(660)), 3);
  assert.equal(readingTimeLabel(words(1100)), "5 min read");
});

test("a markup-heavy body reads the same as its plain equivalent", () => {
  const plain = `<p>${Array.from({ length: 440 }, () => "word").join(" ")}</p>`;
  const wrapped = Array.from({ length: 440 }, () => '<span style="font-weight:700">word</span>').join(" ");
  assert.equal(readingTimeMinutes(plain), readingTimeMinutes(wrapped));
});
