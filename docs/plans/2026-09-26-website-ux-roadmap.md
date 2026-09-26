# Website UX roadmap (2026-09-26)

Every UI/UX and reader-engagement idea proposed for www.aiandtech.news in the
September 2026 review, the decision taken on each, what shipped, and build
notes for the ideas still open. Pick up from here instead of starting over.

- Visual mockups of all 23 ideas: https://claude.ai/artifact/1usZEZqWoWVYAKkEoqDFtm
- The 12 shortlisted ideas previewed on the real site pages, with the
  Build / Later / Skip decisions: https://claude.ai/artifact/MPxtVmMEa4ePHU3npxSEBx
- Glossary definition review: https://claude.ai/artifact/F1DcGF5iBj3dnNsZPfYLdz

## Why: what the research showed

- Search traffic is shrinking. Chartbeat: Google search referrals to news
  sites fell 33% between Nov 2024 and Nov 2025. Pew (July 2025): users click
  a result on 8% of searches with an AI summary against 15% without. So the
  channels the site controls (direct visits, newsletter, push) matter most.
- The second page view is the biggest lever. Chartbeat: first-time visitors
  who view 2 pages return within a week 22% of the time against 8% for 1.
  A personalised read-next widget got 17.7% CTR against 11.6% for plain
  related stories (Twipe).
- Summaries help rather than hurt. Aftonbladet's split tests of AI bullet
  summaries raised read time; about 40% of younger readers use them.
  Schibsted and Hindustan Times saw no drop in engagement (Nieman Lab 2025).
- Topic following drives engagement. FT myFT raised its engagement score 86%
  against a control group (INMA).
- Habits: NYT Games puzzles were played 11.2B times in 2025, and subscribers
  who use news and games together retain best. Quizzes improve recall.
- Discover: `max-image-preview:large` with images of at least 1200 px lifted
  Discover CTR 30 to 79% in Google's case studies.
- Newsletter referrals drove up to 80% of Morning Brew's early growth.

## Decisions and status

| # | Idea | Decision | Status |
|---|------|----------|--------|
| 1 | Homepage share card (generated daily OG image) | Build | Shipped |
| 2 | Feature images at least 1200 px wide for Discover | Build | Shipped (plus a backfill of the last week) |
| 3 | "Most Popular" becomes Trending (Today / This week) | Build | Shipped |
| 4 | TL;DR + "Why it matters" on articles | Build | Shipped |
| 5 | Better read-next (mid-article card + "Keep reading") | Build | Shipped |
| 6 | AI glossary (tap-to-define + term pages) | Build | Shipped |
| 7 | Daily quiz with streak | Build | Shipped |
| 8 | Story clusters (group stories about one event) | Build | **Postponed** (not started) |
| 9 | Company tags on articles | Later | Open |
| 10 | Light theme toggle | Later | Open |
| 11 | Follow topics without an account | Later | Open |
| 12 | Dated "Today in AI" brief (+ audio) | Later | Open |
| 13 | Company / model topic pages (`/topic/openai`) | Skip | Needs tags (#9) first |
| 14 | Headline first, smaller hero image on articles | Not shortlisted | Open |
| 15 | Newsletter form right after the TL;DR, slim sticky bar | Not shortlisted | Open |
| 16 | "Story so far" timelines for developing stories | Not shortlisted | Pairs with #8 |
| 17 | Web push after the 2nd article + installable PWA | Not shortlisted | Open |
| 18 | Model release tracker page | Not shortlisted | Open |
| 19 | "Ask this story" Q&A limited to the article | Not shortlisted | Open |
| 20 | "Who's saying it": company PR vs independent coverage | Not shortlisted | Open |
| 21 | One-tap reactions + daily poll (instead of comments) | Not shortlisted | Open |
| 22 | Newsletter referral rewards | Not shortlisted | Open, once the list passes ~1k |
| 23 | Each newsletter edition as its own shareable page | Not shortlisted | Open |

Standing preferences from the review:

- Never show an article's original source on the site. `source` and
  `source_url` stay stored for duplicate checks and the dashboard only; the
  public article API omits them.
- Never inflate or fake view counts. Trending shows rank and age only.
- Build and ship one feature at a time.

## What shipped, and where it lives

- **Share card:** `apps/web/app/og/today/route.tsx`, `apps/web/app/lib/share-card.ts`.
  Text only: `ImageResponse` cannot read the WebP feature images.
- **Image width:** `imaging.EncodeWebPMinWidth`, used by the illustration
  pipeline (`minFeatureWidth = 1200`).
- **Trending:** migration 008 `article_views` (reads per article per UTC day),
  `GET /api/articles/trending?window=24h|7d` (windowed lists fill up with the
  newest stories), `apps/web/app/components/TrendingTabs.tsx`.
- **TL;DR / Why it matters:** optional fields in the rewrite JSON, dropped
  (not failing the article) when they break the rules; migration 009
  `article_summaries`; `apps/web/app/components/ArticleSummary.tsx`. The
  prompt keeps any limit the source puts on a claim.
- **Read-next:** `apps/web/app/lib/read-next.ts` (shared headline words over
  the last week) and `components/ReadNext.tsx`, which skips stories this
  browser already opened (`lib/visited.ts`) so two related articles don't
  point at each other.
- **Glossary:** 45 approved terms in `apps/web/app/data/glossary.ts`, linked
  at render time by `lib/glossary.ts` (stored bodies stay `<p>`/`<h2>` only),
  `components/GlossaryPopover.tsx`, `/glossary` and `/glossary/[slug]`.
- **Daily quiz:** Go package `internal/quiz` (migration 010 `quizzes`), a
  loop that generates after 04:00 UTC and publishes only when every answer
  is backed by a sentence copied from its article; `GET /api/quiz/today`,
  dashboard `POST /api/dashboard/quiz/pull` and `/regenerate`; web `/quiz`
  and the sidebar `QuizCard`.
- **Layout:** site width 1280 px; on wide screens the right column is 380 px
  with Trending, the quiz card and five "Popular this week" cards (the week's
  most-read, minus today's trending); section labels 12 px.

## Postponed: story clusters (#8)

Show one block per event in the Latest list: the newest story leads, earlier
ones on the same event sit under it ("4 updates since Thursday"). Mockup in
the preview tool. Estimated effort: large.

- Nothing groups stories today: the collector only drops exact URL or slug
  duplicates (`internal/collector/collector.go`), and one article maps to one
  source.
- Add `story_clusters` plus a link table (create-only migrations, as adoption
  requires). At publish time compare the new article with the last ~48 h:
  title-term overlap first, then a cheap Gemini "same event?" yes/no check.
- `ArticleFeed.tsx` renders cluster blocks; a `/story/[slug]` timeline page
  (#16) can follow on the same data.
- Wrong groupings are visible, so add merge/split on the dashboard, or start
  with suggestions an editor confirms.
- Counting other outlets' coverage ("8 outlets") would also need the
  collector to keep matching feed items; the first version can count only
  our own stories.

## Later (#9 to #12): build notes

**Company tags (#9).** New `entities` and `article_entities` tables; ask the
rewrite for an `entities` list (validated like the TL;DR), return it on the
article JSON, backfill existing articles once, edit on the dashboard. Keep a
fixed list of known companies to normalise names. Unlocks topic pages (#13),
following companies (#11) and better read-next (score shared entities
instead of headline words).

**Light theme (#10).** `globals.css` uses `@theme inline`, which bakes hex
values into the utilities: switch to CSS variables with a
`[data-theme=light]` block, replace about 126 hard-coded `text-white` /
`#hex` uses (including the article body's `[&_p]:text-[#e5e5e5]`), and set
the theme with an inline script in `layout.tsx` before first paint. Best done
as its own change.

**Follow topics (#11).** A client component keeps follows, last visit and
read slugs in localStorage (`lib/visited.ts` already records opened
articles). `ArticleFeed.tsx` adds a "For you" tab, new-story dots and dimmed
read stories; a header pill says "N new for you since your last visit".
Following sections works today; following companies needs #9. Render
neutral first, then apply browser state, to avoid a hydration mismatch.

**Today in AI brief (#12).** The newsletter digest already picks five
stories each morning (`internal/newsletter/digest.go`) and saves an edition
(`/api/newsletter/editions`); the homepage can call
`getNewsletterEditions(1)` and fall back to the five newest before the 05:00
UTC cron. One-liners can reuse the TL;DR. Audio needs a TTS provider and a
daily S3 upload: park it.

## Not shortlisted (#14 to #23): short notes

- **#14 Headline first:** the article hero image takes most of the first
  screen on desktop; lead with headline, dek and TL;DR, image smaller.
- **#15 Newsletter after the TL;DR:** email-only form with one line of
  promise ("Get tomorrow's AI brief"), plus a slim sticky bar; no popup on
  load. Content-matched forms convert 2 to 3 times a generic one.
- **#16 Timelines:** a page per developing story linking each update; builds
  on #8.
- **#17 Push / PWA:** ask only after the second article, one notification a
  day at a time the reader picks; add a web app manifest. Media opt-in
  settles around 6 to 8%.
- **#18 Model tracker:** launches, prices and scores, each linked to our
  coverage; evergreen and search-friendly.
- **#19 Ask this story:** suggested questions answered only from the article,
  with paragraph citations.
- **#20 Who's saying it:** split coverage into company statements vs
  independent reporting, like Ground News's bias bar.
- **#21 Reactions + poll:** engagement without moderating comments.
- **#22 Referrals:** personal link, rewards at 1, 3 and 10 referrals.
- **#23 Edition pages:** a full, shareable page per newsletter edition with
  all five stories and a subscribe box.

## Loose ends noticed during the work

- The public article API returns the author's email address
  (`author.email`); it probably shouldn't.
- Trending counts a read whenever the Next server fetches an article (cached
  60 s, includes bots). A client beacon from `ArticleReadTracker.tsx` would
  count real reads.
- Discover: still to add `width`/`height` to the OG image and the article
  JSON-LD image.
- `/gaming` is in the navigation but the publisher never assigns Gaming.
- The quiz pull/regenerate actions exist as API calls only; no dashboard
  buttons yet.
- Articles published before 2026-09-26 have no TL;DR; a one-off backfill of
  recent ones is possible.
- Polling www.aiandtech.news with curl trips Vercel's bot challenge (403);
  check live pages in a browser.
