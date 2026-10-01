# Original value: explainers, analysis and topic hubs

Date: 2026-10-01
Status: plan, not started

## Why

Google search sends about 33 sessions a week. Every article is a rewrite of
another outlet's story, and Google ranks the original above a rewrite and
demotes sites that mostly rephrase others (scaled content, helpful content).
More rewrites will not grow organic traffic. Pages need something the source
does not have, and some pages need to stay useful for months, not a day.

Three additions, in this order:

1. **Topic hubs**: one page per company, product or theme that collects our
   coverage under a maintained summary. Cheapest, uses what we already have.
2. **Analysis on each story**: a short original section per article that the
   source does not contain.
3. **Evergreen explainers**: standalone pages answering questions people keep
   searching for.

Rules for all three (unchanged house rules):

- Never show, link or credit the original source anywhere on the site.
- Facts only from material we hold (our articles, their stored source text,
  official pages); no invented numbers, quotes or claims.
- AI drafts, a check step verifies, nothing ships that fails the check.
- Each page has one clear search intent, its own title and description, and
  structured data.

---

## 1. Topic hubs

**What**: `/topics/<slug>`, e.g. `/topics/nvidia`, `/topics/openai`,
`/topics/humanoid-robots`, `/topics/gpt-6`.

Each hub has:

- An H1 and a 150-250 word summary: what it is, where things stand now, the
  latest key developments. Regenerated when new coverage arrives (at most
  daily).
- A "Key facts" list (founded, products, latest model, etc.), only from our
  coverage.
- A timeline: our articles about it, newest first, grouped by month.
- Related topics.

**How topics are chosen**

- The publisher already knows each story's maker / subject (realphoto plan,
  illustration brief). Add an entity extraction step after publish: 1-4
  entities per article (company, product, person, theme) with a canonical
  name and type, stored in an `article_topics` table (new create-only
  migration: `topics(id, slug, name, kind, summary, facts_json, updated_at)`,
  `article_topics(article_id, topic_id)`).
- A hub page goes live only when a topic has 3+ articles, so no thin pages.
  Backfill all ~400 existing articles once.
- Normalise aliases ("OpenAI Inc", "Open AI" -> openai) with a small alias
  table plus the model.

**Summary generation**

- Input: titles, TL;DRs and why-it-matters lines of the topic's last 20
  articles. Output: summary + facts as JSON. Check: every fact must be
  traceable to one of the inputs (second model call verifies, like the image
  compliance check); otherwise drop the fact.

**SEO**

- Linked from every article that mentions the topic (chips under the title,
  and the first mention in the body).
- In the sitemap; `CollectionPage` + `ItemList` structured data.
- Topic pages update often, which signals freshness.

**Size**: medium. Go: extraction step, migration, summary job, API
`/api/topics`, `/api/topics/:slug`. Web: hub page, chips on articles,
sitemap.

---

## 2. Original analysis section per article

**What**: a short block in each new article, after the body:
**"What this means"** (or "Our take"), 80-150 words, 2-4 sentences.

Content types the model may write, picking the one that fits:

- **Context**: how this compares with earlier moves, using our own past
  articles on the same topic (from the topic tables above).
- **Implications**: who is affected and how, limited to what the reporting
  supports.
- **What to watch**: the next concrete milestone already stated in the
  reporting.

**Guardrails**

- It must not repeat the TL;DR or why-it-matters line.
- No speculation presented as fact, no predictions without a stated basis,
  no invented numbers. The check step rejects sentences not supported by the
  article, its source text or our linked past articles.
- When nothing useful can be said, skip it. No filler.
- Internal links to the 1-3 past articles it draws on. This is good for SEO
  and for read-next.

**Where**: generated in the publisher after the rewrite (same place as the
subheading pass), stored in a new table (columns cannot be added to existing ones,
so a new create-only table `article_analysis(article_id, kind, text,
links_json)`), returned in the article JSON, rendered as a styled block.

**Size**: small-medium once topic hubs exist (it needs related past
articles).

---

## 3. Evergreen explainers

**What**: `/explainers/<slug>` pages answering searches with steady demand:

- "What is X": GPT-6, Claude Opus 5.5, RTX PRO 6000, Wi-Fi 7, MCP, AI
  agents, humanoid robots.
- Comparisons: "RTX PRO 6000 vs RTX 5090", "GPT-6 vs Claude Opus 5.5".
- How-to / guides: "How to run AI models locally on a Mac", "Best open
  models for coding".

Each explainer: 800-1500 words, clear H2 sections, a summary box at the top,
an FAQ section (`FAQPage` structured data), a "Last updated" date, and links
to our news coverage and the topic hub.

**Choosing subjects**

- Start from topics with the most coverage and the glossary's 45 terms
  (several glossary terms can become explainers).
- Use Search Console queries (once connected) to find what people already
  search and land on.
- 10 explainers in the first round; then about 2 per week.

**Writing and checking**

- Sources: our articles on the topic, the stored source texts, official
  pages (product pages and docs), never other news sites' wording.
- Draft -> fact check (every claim mapped to a source passage, unsupported
  claims removed) -> house style check (no em dashes, no hype words).
- Explainers are reviewed by you before publishing for the first round; a
  draft queue in the dashboard (status draft, Publish button).
- Refresh: when a topic gets significant news, mark the explainer stale and
  regenerate the affected sections; "Last updated" changes only then.

**Size**: medium-large (new content type, dashboard review queue, pages).

---

## Rollout

| Phase | Work | Success signal |
|---|---|---|
| 0 | Connect Search Console data (queries, pages) to see a baseline | Baseline recorded |
| 1 | Topic extraction + backfill, hub pages, chips, sitemap | 30+ hubs live, indexed |
| 2 | Analysis section on new articles | Shown on 70%+ of new articles, 0 check failures shipped |
| 3 | 10 explainers, review queue | All 10 indexed |
| 4 | Explainers cadence ~2/week, refresh job | Organic sessions trend up |

Measure after 4 and 8 weeks: organic sessions, impressions and clicks per
page type (article / hub / explainer), pages indexed, internal link clicks.
Target: organic search sessions x3 in 8 weeks.

## Risks

- **Thin or duplicate hubs**: the 3-article minimum and canonical aliases
  avoid near-empty pages.
- **AI errors in analysis or explainers**: the fact-check step, plus manual
  review of explainers at first.
- **Cost**: a few extra model calls per article and per hub refresh; small
  next to image generation.
- **Over-linking**: cap topic chips at 4 and in-body links at 3 per article.
