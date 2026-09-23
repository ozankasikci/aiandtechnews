# News Publishing Policy

This file is the source of truth for every article added to this repository, whether the work is performed manually or by automation. Read it before selecting, writing, importing, or publishing news.

## Editorial queue (Go newsroom)

`apps/server-go`'s newsroom (`internal/collector`, `internal/newsroom`, `internal/publisher`) is the eventual replacement for the legacy Node importer described below. It works as an editorial queue, not a direct publisher:

- Collection never publishes. The collector fetches the approved feeds below, applies this policy, and stores every item that passes as a pending candidate for review. A rejected candidate is never collected again.
- An editor selects candidates for publishing in the Omni Control app.
- Selected candidates publish one at a time, spaced by a random delay (default 30-40 minutes, configurable) from the previous publish, and the publisher additionally enforces a minimum gap equal to the configured minimum since the last publish before it will claim the next candidate.
- The Node importer and its external scheduler (see "Daily automation contract" below) remain the active publisher in production until the Go publisher is enabled there, a later phase. **The two must never run at the same time**, since running both concurrently risks duplicate or conflicting publishes.

## Source selection

Use RSS items from these publications only:

| Publication | Feed |
| --- | --- |
| TechCrunch | `https://techcrunch.com/feed/` |
| The Verge | `https://www.theverge.com/rss/index.xml` |
| Ars Technica | `https://feeds.arstechnica.com/arstechnica/index` |
| WIRED | `https://www.wired.com/feed/rss` |
| Engadget | `https://www.engadget.com/rss.xml` |
| BleepingComputer | `https://www.bleepingcomputer.com/feed/` |
| The Register | `https://www.theregister.com/headlines.atom` |
| MIT Technology Review | `https://www.technologyreview.com/feed/` |
| VentureBeat | `https://venturebeat.com/category/ai/feed` |
| 404 Media | `https://www.404media.co/rss/` |
| Rest of World | `https://restofworld.org/feed/` |
| Decrypt | `https://decrypt.co/feed` |
| The Decoder | `https://the-decoder.com/feed/` |
| ZDNET | `https://www.zdnet.com/topic/artificial-intelligence/rss.xml` |
| InfoQ | `https://feed.infoq.com/ai-ml-data-eng/` |
| IEEE Spectrum | `https://spectrum.ieee.org/feeds/topic/artificial-intelligence.rss` |
| SiliconANGLE | `https://siliconangle.com/category/ai/feed/` |
| AI Business | `https://aibusiness.com/rss.xml` |
| ScienceDaily | `https://www.sciencedaily.com/rss/computers_math/artificial_intelligence.xml` |

- Never use Hacker News as a source or discovery feed.
- Do not create or publish a Deals category.
- Reject retail deals, coupons, sales roundups, buying guides based on discounts, preorder offers, bonus offers, "last chance" pitches, and other promotional items. Do not reject a business transaction merely because its headline uses `deal` to describe an investment, acquisition, partnership, or infrastructure agreement. Apply promotional checks to both the RSS headline and normalized canonical URL words.
- Reject product reviews, hands-on pieces, buying guides, editorial roundups, and recurring mixed-topic columns such as Installer. The site publishes news, not review or recommendation content.
- Reject Show HN posts, PDFs, videos presented as the item itself, abstracts, arXiv-style entries, and obviously old reposts.
- A manual article must also originate from one of the approved RSS feeds. A permitted domain by itself is not enough.
- Exception for explicit human approval: an article that was presented from an approved feed and explicitly selected by Ozan may still be imported after it ages out of that feed's current item window, but only through the dedicated `--approved-aged-out` path with its approved source headline and original publication timestamp. Those assertions must match authoritative metadata fetched from the canonical article page, the item must be no more than seven days old, and it must pass every other source, AI-topic, format, accessibility, image, content, and duplicate check. This exception never applies to automatic imports or unapproved substitutes.
- **Every publication path is AI-only.** Both automatic and manual imports must have an explicit AI signal in the RSS headline or a specific AI section in the canonical URL. Accepted signals include AI, artificial intelligence, machine learning, LLMs, major AI labs or assistants, neural networks, and foundation or frontier models.
- Generic technology signals such as software, cybersecurity, chips, apps, startups, robotics, or a `/tech/` section are not sufficient unless the story is clearly about AI.
- General technology, science, space, entertainment, business, and other non-AI stories must be rejected even when they appear in an approved publication's RSS feed.
- AI classification must fail closed: if AI relevance is unclear, skip the story rather than using it to fill a scheduled slot or allowing a manual bypass.
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
- Re-run the promotional-language check against the rewritten headline. A rewrite must never turn an accepted source headline into promotional copy.
- Write one plain-sentence excerpt with no HTML and no more than 180 characters.
- Target 150 to 800 words, generally in 5 to 12 paragraphs.
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

These rules apply to the Go publisher (`internal/illustration`, backed by `internal/media`'s S3 storage). The legacy Node importer on `main` still hotlinks the source `og:image` and is not held to these rules until it is retired.

1. Every newly published article requires an original generated illustration stored in the S3 feature image bucket we own. The source `og:image` is never the published image.
2. The source `og:image` is only a visual reference. When one is available and usable, it is fetched into memory (with an image content-type check, a size cap, and a request timeout) and passed to the image model as an inline image next to the text prompt. It is never written to disk, uploaded, stored, or hotlinked. When no usable source image exists, the illustration is generated from the article text alone.
3. The reference is normalized in memory before use: decoded, resized to fit inside 1024x1024 without enlarging it, and re-encoded as JPEG so metadata is stripped and the request stays small. A reference that cannot be decoded this way is dropped, and the illustration is generated from the article text alone.
4. The illustration prompt asks for an original editorial illustration that preserves the core subject and colour palette of the reference image while creatively changing composition, perspective, staging, poses, spacing, and visual narrative. It forbids tracing the source or reproducing its layout, and forbids copying any source text, captions, logos, watermarks, or branding.
5. The prompt forbids writing of every kind: no words, letters, numbers, digits, emoji, symbols, speech or thought bubbles, signs, banners, posters, readable screen content, or writing in any language or script. Any surface that would normally carry writing must be left blank or filled only with abstract marks.
6. The prompt forbids inventing physical states or events: no injury, wounds, blood, damage, violence, or visible distress unless the article states it, and no demeaning or mocking caricature of a real, identifiable person.
7. The illustration uses a bright, bold, thick-outlined cartoon treatment in a landscape composition suitable for a news hero image.
8. The illustration must match the specific subject and factual tone of the article. It must never depict facts, events, products, or people that the article does not support.
9. Gemini generates and reviews the image (`GEMINI_IMAGE_MODEL`, `GEMINI_VISION_MODEL`).
10. The generated image is encoded as WebP and uploaded to the S3 feature image bucket with the key `<S3_FEATURE_IMAGE_PREFIX>/YYYY/MM/<slug>-<content-hash>.webp` (prefix defaults to `features`); the complete verified public URL is saved as the article's featured image.
11. If a generation call that carried the reference image fails, the next call in the same generation budget retries from the article text alone (the reference is dropped); a text or logo compliance rejection also drops the reference for the next attempt.
12. Every generated image passes an automated compliance check with a vision model before upload: a strict verdict on whether it contains writing, a logo or watermark, or unsupported injury/violence. The check fails closed — an unparsable, incomplete, or errored verdict counts as non-compliant and the image is regenerated.
13. At most three image generation calls run per article, counting any text-only retry after a failed referenced call. When no compliant image is produced within that limit, the candidate is marked failed and is not retried automatically; an editor can retry it in the app.
14. Fail closed. If generation, upload, or the public retrieval verification fails, the article is not published. There is no fallback to the source image and no placeholder.
15. If the article is not published after the upload succeeded, the uploaded S3 object is deleted on a best-effort basis and a delete failure is logged without masking the original error.
16. Duplicate `source_url`/slug checks run before generating and uploading the image, and are repeated immediately before insertion.
17. Historical articles keep their existing image URLs. Do not rewrite, migrate, or delete them.

## Daily automation contract

This section, including the "one article per run" and "four slots a day" limits, applies only to the legacy Node importer while it remains the active publisher (see "Editorial queue (Go newsroom)" above). It does not describe or constrain the Go publisher.

- The external scheduler checks every five minutes in `Europe/Istanbul` and maintains four randomized publication slots per calendar day.
- Generate one slot in each window: 06:30 to 10:00, 10:30 to 14:00, 14:30 to 18:00, and 18:30 to 22:00.
- Target four successfully published articles per day, while publishing at most one new article per importer run.
- The repository entrypoint is `pnpm --filter @technews/server news:daily`.
- Run with `IMPORT_ONLY=1` and `MAX_IMPORTS=1`. The importer must clamp every individual run to one article even if a larger environment value is supplied.
- A slot is complete only when the database count increases by exactly one and the inserted row is identified. A run that publishes no compliant article must retry that slot after 30 minutes.
- An import-only run must not trigger bulk cleanup, rewriting or enhancement of older articles, or image backfills.
- Importer and scheduler locks must prevent overlapping runs.
- A rejected candidate is skipped. It must not be replaced with unverified or lower-quality material merely to fill a slot.
- Every candidate must pass the AI-only topic gate before fetching, rewriting, or insertion. The daily scheduler and manual importer must enforce the same boundary.
- A failed rewrite, failed validation, missing database field, or other unsafe state must make the run fail or skip the candidate. It must never silently publish fallback text.
- After insertion, verify the exact article through the public API and canonical public page. A verification warning does not permit a second insertion for the same slot.

The scheduler is external to this repository. Its operational status is a snapshot, not a guarantee. Verify the external scheduler, current daily state, and latest run directly whenever current status matters. Do not infer scheduler health from this repository alone.

## Manual publishing checklist

Before publishing manually:

1. Confirm the item is clearly AI-related, came from a current approved RSS feed, and is not a rejected item type. If an explicitly selected item has since aged out of the feed window, use only the bounded approved-aged-out path described above.
2. Confirm it is news, not a deal or promotion.
3. Check the canonical `source_url` and proposed slug for duplicates.
4. Verify every factual claim and quotation against the source.
5. Validate headline, excerpt, length, paragraph count, HTML tags, and prohibited language.
6. Confirm the author is `TechNews Editorial` and status is `published`.
7. Confirm `source` and `source_url` are populated and no source footer appears in the body.
8. Use the source `og:image` when usable, or generate a subject-matched fallback image.
9. Read the stored article back from the database or API and confirm all fields after insertion.

When these rules change, update this document and the matching importer validation in the same change.
