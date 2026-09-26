import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { GLOSSARY } from "../data/glossary";
import { glossaryTerm, linkGlossaryTerms, mentionsTerm } from "./glossary";

const link = (slug: string, text: string) => `<a href="/glossary/${slug}" class="glossary-term" data-glossary="${slug}">${text}</a>`;

test("every glossary entry is unique, plain text and within the length limit", () => {
  const slugs = new Set<string>();
  const names = new Set<string>();
  for (const entry of GLOSSARY) {
    assert.match(entry.slug, /^[a-z0-9-]+$/);
    assert.ok(!slugs.has(entry.slug), `duplicate slug ${entry.slug}`);
    slugs.add(entry.slug);
    for (const name of [entry.term, ...entry.aliases]) {
      assert.ok(!names.has(name.toLowerCase()), `name used twice: ${name}`);
      names.add(name.toLowerCase());
    }
    assert.ok(!/[—<>]/.test(entry.definition), `${entry.slug} has an em dash or markup`);
    assert.ok(entry.definition.length <= 240, `${entry.slug} definition is too long`);
  }
  assert.equal(glossaryTerm("ai-agent")?.term, "AI agent");
  assert.equal(glossaryTerm("missing"), undefined);
});

test("only the first mention of each term is linked, keeping the article's own wording", () => {
  const { html, slugs } = linkGlossaryTerms("<p>AI Agents are here. More AI agents came.</p><p>An AI agent again.</p>");
  assert.equal(html, `<p>${link("ai-agent", "AI Agents")} are here. More AI agents came.</p><p>An AI agent again.</p>`);
  assert.deepEqual(slugs, ["ai-agent"]);
});

test("the longest name wins where terms overlap, and aliases link to their term", () => {
  const { html, slugs } = linkGlossaryTerms("<p>Coding agents and open-weight models use LLMs.</p>");
  assert.equal(
    html,
    `<p>${link("coding-agent", "Coding agents")} and ${link("open-weight-model", "open-weight models")} use ${link("large-language-model", "LLMs")}.</p>`,
  );
  assert.deepEqual(slugs.sort(), ["coding-agent", "large-language-model", "open-weight-model"]);
});

test("acronyms match only in capitals, and whole words only", () => {
  const { html } = linkGlossaryTerms("<p>The rapid api said agile agility helps; the API changed.</p>");
  assert.equal(html, `<p>The rapid api said agile agility helps; the ${link("api", "API")} changed.</p>`);
});

test("headings, tag attributes and existing links are left alone", () => {
  const body = '<h2>Why AI agents matter</h2><p>See <a href="/x">AI agents here</a> and inference.</p>';
  const { html } = linkGlossaryTerms(body);
  assert.equal(html, `<h2>Why AI agents matter</h2><p>See <a href="/x">AI agents here</a> and ${link("inference", "inference")}.</p>`);
});

test("the article page links terms in the body and mounts the popover for them", () => {
  const page = readFileSync(new URL("../article/[slug]/page.tsx", import.meta.url), "utf8");
  assert.match(page, /linkGlossaryTerms\(/);
  assert.match(page, /<GlossaryPopover /);
  const popover = readFileSync(new URL("../components/GlossaryPopover.tsx", import.meta.url), "utf8");
  assert.match(popover, /createPortal\(/, "the popover must be positioned against the page, not the article container");
});

test("term pages find stories by the same whole-word, case-aware matching", () => {
  const api = GLOSSARY.find((e) => e.slug === "api")!;
  assert.equal(mentionsTerm(api, "Developers get a new API."), true);
  assert.equal(mentionsTerm(api, "The capital city of rapid growth."), false);
  const agent = GLOSSARY.find((e) => e.slug === "ai-agent")!;
  assert.equal(mentionsTerm(agent, "Two AI agents met."), true);
  assert.equal(mentionsTerm(agent, "Coding agents met."), false);
});

test("the glossary is in the sitemap and linked from the footer", () => {
  const sitemap = readFileSync(new URL("../sitemap.ts", import.meta.url), "utf8");
  const footer = readFileSync(new URL("../components/Footer.tsx", import.meta.url), "utf8");
  assert.match(sitemap, /glossary\/\$\{entry\.slug\}/);
  assert.match(footer, /href: "\/glossary"/);
});
