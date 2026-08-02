# News Publishing Policy

This file is the source of truth for every article added to this repository, whether the work is performed manually or by automation. Read it before selecting, writing, importing, or publishing news.

## Source selection

Use RSS items from these publications only:

| Publication | Feed |
| --- | --- |
| TechCrunch | `https://techcrunch.com/feed/` |
| The Verge | `https://www.theverge.com/rss/index.xml` |
| Ars Technica | `https://feeds.arstechnica.com/arstechnica/index` |

- Never use Hacker News as a source or discovery feed.
- Do not create or publish a Deals category.
- Reject deals, coupons, sales roundups, buying guides based on discounts, and other promotional items.
- Reject Show HN posts, PDFs, videos presented as the item itself, abstracts, arXiv-style entries, and obviously old reposts.
- A manual article must also originate from one of the three approved RSS feeds. A permitted domain by itself is not enough.
- Preserve the canonical source URL. Do not replace it with a search result, aggregator, tracking URL, or home page.

Before writing, confirm both of these are new:

1. The canonical `source_url` is not already stored.
2. The generated article slug is not already stored.

Repeat both checks immediately before insertion. A rewritten headline can produce a different final slug, so check the final slug too.

## Writing contract

Rewrite the source into an original, human-sounding article. Do not copy the source article or closely imitate its phrasing.

- Preserve facts, names, numbers, dates, and quotations accurately.
- Never invent a quotation, statistic, motive, consequence, or unsupported detail.
- Use a conversational but factual voice, as if explaining the story to a friend.
- Prefer short, punchy sentences. Remove filler.
- Never use an em dash.
- Do not use the phrases `In a move that` or `It remains to be seen`.
- Do not describe a development as `groundbreaking`, `revolutionary`, or `game-changing`.
- Write a short, direct, factual, non-clickbait headline.
- Write one plain-sentence excerpt with no HTML and no more than 180 characters.
- Target 400 to 600 words, generally in 5 to 8 paragraphs.
- Start with a clear lede explaining what happened.
- Include relevant context and background.
- Explain industry implications without presenting speculation as fact.
- End by explaining the next known step. If no next step is known, say only what can be supported by the source.
- Article body HTML may contain `<p>` and optional `<h2>` elements only.

If the available reporting cannot support a complete, accurate article, skip the item. Never publish a raw feed excerpt, an unrevised source body, or an AI response that fails validation.

## Attribution and publication

- Preserve the publication name in `source`.
- Preserve the canonical original article URL in `source_url`.
- Attribution belongs in those fields. Do not append a source or sources line to the article body.
- Insert imported articles with `status = published`.
- Use `TechNews Editorial` as the author. Do not invent journalist identities or rotate fictional personas.

## Images

1. Use the source article's usable `og:image` URL by hotlinking it. Do not download or copy the source image into this repository.
2. Generate an image only when the source has no usable `og:image`.
3. A generated image must match the specific subject and factual tone of the article.
4. Use Gemini or Nano Banana for fallback image generation.

Do not replace a usable source image merely to make the article look more consistent with the site.

## Daily automation contract

- The active job runs daily at 09:00 in `Europe/Istanbul`.
- Publish at most one new article per run. The importer must enforce this even if a larger environment value is supplied.
- Run with `IMPORT_ONLY=1`.
- An import-only run must not trigger bulk cleanup, rewriting or enhancement of older articles, or image backfills.
- An importer lock must prevent overlapping runs.
- A rejected candidate is skipped. It must not be replaced with unverified or lower-quality material merely to publish something that day.
- A failed rewrite, failed validation, missing database field, or other unsafe state must make the run fail or skip the candidate. It must never silently publish fallback text.

The scheduler is external to this repository. As reported on 2026-08-02, the cron job is enabled and its most recent run succeeded. That operational status is a snapshot, not a guarantee. Verify the external scheduler and its latest run directly whenever current status matters. Do not infer scheduler health from this repository alone.

## Manual publishing checklist

Before publishing manually:

1. Confirm the item came from a current approved RSS feed and is not a rejected item type.
2. Confirm it is news, not a deal or promotion.
3. Check the canonical `source_url` and proposed slug for duplicates.
4. Verify every factual claim and quotation against the source.
5. Validate headline, excerpt, length, paragraph count, HTML tags, and prohibited language.
6. Confirm the author is `TechNews Editorial` and status is `published`.
7. Confirm `source` and `source_url` are populated and no source footer appears in the body.
8. Use the source `og:image` when usable, or generate a subject-matched fallback image.
9. Read the stored article back from the database or API and confirm all fields after insertion.

When these rules change, update this document and the matching importer validation in the same change.
