# Real photos for featured images

Date: 2026-11-24
Repo: `aiandtechnews` (`apps/server-go`, `apps/web`)
Status: draft for owner review

## Summary

The featured image is the picture on the homepage, on section pages and at the
top of an article. Today it is always a generated illustration. This change
makes the publisher look for a real, credited photo first and draw an
illustration only when no photo passes the checks.

The owner reviewed three mock homepages on 2026-11-24 and chose real photos
over two treatments of generated art. The mock used 12 published stories: 9
got a Wikimedia Commons photo and 3 got the company's own image. The photos in
the mock were picked by hand, so the automated result will be worse until the
checks below are tuned.

## Why

- The current images read as generated, and no two share a look. There are 11
  house styles and 8 palettes.
- A 2026 experiment with 1,171 participants found that generated images
  lowered trust in a news outlet compared with real photos, whatever the
  disclosure.
- The site already finds real photos (`internal/realphoto`), but only for the
  image inside the article body.
- The photo planner accepts only a physical subject. It rejects software, AI
  models, funding rounds, lawsuits and policy, which is most of what the site
  publishes.

## What changes

1. The featured-image chain gets a new first step, `photo`.
2. The photo planner proposes a stand-in subject when the story has nothing
   physical to show.
3. The vision check gets stricter rules for the featured slot.
4. A featured photo's credit is stored and shown on the article page.
5. `NEWS_PUBLISHING_POLICY.md` is updated to allow all of this.

Nothing changes until `FEATURED_IMAGE_CHAIN` names the new step.

## The photo step

`FEATURED_IMAGE_CHAIN` accepts a fourth provider name, `photo`. The intended
production value is `photo,codex,gemini`. The default stays as it is.

The step calls `realphoto.Finder` with the article's slug, title, text and
source URL. The Finder tries, in this order:

1. **The exact subject.** A Commons photo of the product, person, building or
   event the story is about, then the maker's own image from its official
   page. This is today's behaviour.
2. **A stand-in subject.** Used only when the planner says the story is not
   photographable. See the next section.

A found photo is cropped to 16:9, encoded as WebP and stored in our S3 bucket
like any featured image. Nothing is hotlinked.

When no photo passes, the step fails and the chain moves to the next provider,
so the article still gets a drawn illustration. The brief analyzer runs only
when the chain reaches a generating provider, so a story that gets a photo
makes no analyzer call.

Runs that must produce our own drawn image (`generatedOnly`, used by the
re-image job) skip the step, as they skip `source`.

## Stand-in subjects

When `photographable` is false, the planner also returns up to three stand-in
subjects, most suitable first. Each has a subject, a kind and two Commons
search queries.

| Kind | When it fits | Example |
|---|---|---|
| `person` | The story covers a company with one clear leader and that company's funding, leadership change, earnings, acquisition or lawsuit | Sierra's funding round: Bret Taylor |
| `place` | The story is about the company itself, or a regulator or court | An SEC inquiry into Nvidia: Nvidia's headquarters |
| `product_in_use` | The story is about a software product that people see on a device | A ChatGPT model rollout: ChatGPT on a phone |

The planner returns none when no stand-in is honest. A story about a study, a
market trend or several companies at once has none.

The planner always returns a `story_type` from `funding`, `leadership_change`,
`earnings`, `acquisition`, `lawsuit`, `product`, `research`, `policy` and
`other`. Product and model launches, rollouts, features, updates, pricing
changes and availability changes are `product`. The parser treats missing or
unrecognized types as `other` and keeps leader stand-ins only for the five
corporate actions. Place and product-in-use stand-ins remain eligible for
every story type. A person who is the exact subject remains eligible too.

Stand-ins come from Commons only. The official-page step is not used for them,
because a company's page for an unrelated subject is the wrong source.

Commons capture years come from the first plausible year in the file's
`DateTimeOriginal` metadata after stripping HTML. A year must be between
1900 and the current year. If the metadata has no readable year, the capture
year is unknown. Before any download or vision check, the finder rejects
person photos with an unknown capture year or a year more than five years
old in either slot, including leader stand-ins. For other featured Commons
photos, the finder rejects a known capture year more than ten years old and
allows an unknown year.

## Checks for the featured slot

The vision check (`realphoto/verify.go`) takes the slot as a parameter. For
the featured slot it differs from the in-body check in these ways:

- **Quality** must be 8 or more. The in-body minimum stays 7.
- **Match** must be `exact` against the subject searched for. For a stand-in
  that is the stand-in subject.
- **Dated** is a new field: true when the hardware, interface or setting is
  visibly out of date. A dated photo is rejected. In the mock, an old phone
  and a 1980s laboratory were the weakest picks.
- **Event branding** is allowed for a `person` subject. A speaker in front of
  a conference backdrop is a normal press photo. `other_branding` still
  rejects every other kind.
- **Focus** is a new field: the point, as fractions of width and height, that
  the 16:9 crop must keep in frame. The crop is centred on it. Without it a
  portrait loses the head.

The existing rejections stay: watermark, text-heavy, marketing banner,
misleading, subject not prominent. In the mock they would have rejected two of
the three company images, which were branded cards with text.

**Rate limits.** Commons answered HTTP 429 after about 25 quick requests while
the mock was built with a script that did not identify itself. The Finder
already sends a User-Agent naming the site. It must also treat a 429 as no
photo for that query, not as an error.

## Variety

The finder excludes identical featured photo files for 30 days using their
source image URLs. For stand-ins, it also skips a search when a featured
stand-in from the last seven days has the same kind and either the same
subject or the same non-empty maker. Subject and maker comparisons ignore
case and collapse whitespace. The finder tries the remaining stand-ins and
reports the skip if no photo is found. Exact subjects follow the file rule
only.

Migration 19 adds `article_featured_stand_ins` to store the subject, kind and
plan maker in the same transaction as the article and photo credit. A failed
history read is logged and the photo step continues with an empty history
for that window.

## Credits

A featured photo's credit needs its own table, because the existing
`article_image_credits` table is keyed to the in-body image and a migration
may only add new objects.

```sql
CREATE TABLE article_featured_image_credits (
    article_id INTEGER PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('commons', 'official')),
    credit TEXT NOT NULL,
    credit_url TEXT NOT NULL DEFAULT '',
    license TEXT NOT NULL DEFAULT '',
    license_url TEXT NOT NULL DEFAULT '',
    alt TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
```

A generated featured image has no row. The publisher writes the row in the
same transaction that inserts the article.

The article API returns the credit with the article, and
`contracts/openapi.yaml` gains the field. The headline block overlaps the
bottom of the featured image, so the article page places the credit in the
top right corner on a dark backing for legibility. The credit reuses the
in-body caption code (`apps/web/app/lib/inline-images.ts`). Cards on list
pages show no credit.

The in-body image worker is told the featured photo's credit URL and never
picks the same photo.

## Policy

The Images section of `NEWS_PUBLISHING_POLICY.md` changes as follows:

- Rule 2 lists `photo` as a provider and says what it may use: a freely
  licensed Commons photo, or the maker's own official image.
- A new rule states that a photo must show the article's subject or a
  stand-in of the three kinds above, must be credited, and must never come
  from a news source.
- Rules 4 to 6 are marked as applying to generated images only. A real photo
  of a real person is allowed. A generated likeness is still forbidden.
- Rule 12 is unchanged. Published articles keep their images.

## Trying it before it goes live

A new command, `cmd/photo-try`, follows the existing `-try` commands. Given
slugs, or a count of recent articles, it runs the photo step without
publishing and writes an HTML report. For each article the report shows the
current image, the chosen photo with its credit, the vision verdict, and the
rejected candidates with the reason for each. It shows capture years for
Commons candidates and identifies recently used stand-ins on misses. It
applies both reuse windows from the read-only database copy and tracks photos
and stand-ins chosen earlier in the same run. The copy needs migration 19.

The step is enabled in production only after the owner has reviewed a report
covering at least 30 recent articles. Once live, every publish logs whether
the image came from `photo` or from a generating provider, so the share of
stories that fall through to a drawing is known.

## Testing

Each change is tested with fakes, as the packages already do:

- Planner: stand-ins are parsed, capped at three, and empty for a
  photographable story.
- Verify: the featured slot applies the quality, dated and event-branding
  rules; the in-body slot is unchanged.
- Crop: the focus point stays inside the 16:9 crop at every edge.
- Finder: a photo used in the last 30 days is skipped; a 429 yields no photo.
- Pipeline: `photo` succeeds without an analyzer call; `photo` failing hands
  over to the next provider; `generatedOnly` skips it.
- Store and API: the credit row is written with the article and returned by
  the article endpoint.
- Web: the article page renders the credit, and renders nothing for a
  generated image.

## Out of scope

- **A house photo library.** Photos the owner shoots for recurring subjects
  would make the site look like no other. It is a separate spec.
- **The drawn illustrations.** Their styles and palettes are not changed here.
- **Old articles.** No backfill.
- **A typographic card** as the last resort instead of a drawing.

## Decisions for the owner

The spec takes a position on each of these. Say so if any is wrong.

1. **Fall back to a drawing.** When no photo passes, the article gets today's
   generated illustration. The alternative is a typographic card, which needs
   a new renderer.
2. **Company images are allowed when they are photographs.** They are
   published for press use, which is common practice but not a licence.
3. **Credits appear on the article page only.** Cards, social previews and
   Telegram posts carry no credit. Commons licences ask for attribution in a
   reasonable manner; the article page is the usual reading of that.
4. **A stand-in photo is not labelled as a file photo.** Its alt text says
   what it shows, and its credit links to the original.
