# In-article illustration: the next moment

Date: 2026-10-15
Status: approved design, not built

## Why

Covers improved on 2026-10-12 (thumbnail-first, no robots or app windows, a
scene note history so each cover differs from the last 10). The second
illustration inside the article body shares most of those prompt rules but
still repeats stock scenes: on 2026-10-15 the Figma Specs article got "a man
at a desk sorting a stack of papers". Two gaps cause it:

- The in-article brief has no memory of earlier images, so nothing stops the
  same kind of scene from coming back.
- For software stories the "show the people involved" rule falls back to a
  person at a desk.

Number callouts were considered and dropped by the owner the same day.

## What changes

### 1. The image's job: the next moment

The in-article brief (`BuildInlineAnalyzePrompt`) asks for the next concrete
moment of the passage beside the image: its consequence, the people affected,
or the place where it happens. It must not repeat the cover's subject,
setting or framing (the cover is already attached to the analyzer).

Banned unless the passage is literally about it: a person at a desk with
papers, a person at a keyboard or screen, a handshake, a humanoid robot, an
app window or dashboard.

### 2. Memory of recent images

- A second history, settings key `newsroom.inline_image_history`, same shape
  and size (10) as the cover history, written after each stored inline
  drawing (not for real photos). Each entry keeps `SceneNote(brief)`.
- The inline analyzer input gets `RecentScenes` = the last 10 cover scene
  notes plus the last 10 inline scene notes, newest last, and uses the
  existing `RecentScenesRule` text (different subject, setting and pose;
  a repeated kind of scene is used up).
- History errors only cost variety; they never fail the image.

### 3. Redraw the 15 re-covered articles

A command, `cmd/inline-redraw`, takes slugs (or `-latest N`) and the DB path:

- skips an article whose inline image has a credit row (Commons or official
  photo) or whose featured image is not ours;
- otherwise draws a new inline image with the new rules, using the article's
  current cover as the style reference, stores it like the worker does, and
  updates `article_images.url` and `alt` in place (same paragraph slot);
- `-dry-run` writes the images to a folder and changes nothing;
- before any write it saves the old url/alt per article to a JSON file, so the
  change can be undone;
- after writing it calls the site revalidation for those slugs (no lists).

The target set is the 15 articles whose covers were replaced on 2026-10-12
(`covers3/plan.json` on the mini). New articles get the new rules through the
existing worker.

## Testing

- Unit tests: inline history record/recent; inline input carries cover and
  inline scene notes; the inline prompt contains the next-moment and banned
  scene rules; the redraw command skips credited photos and writes the undo
  file before updating.
- Dry run on the 15 articles, shown to the owner as old vs new, before deploy
  and before any live update.

## Out of scope

Number callouts, charts, tables, layout changes, re-drawing older articles.
