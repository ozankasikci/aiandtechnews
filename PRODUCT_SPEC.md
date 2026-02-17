# PRODUCT_SPEC.md — TechNews Product Specification

This document is the single source of truth for building the TechNews website and dashboard. A frontend engineer should be able to implement every page, component, and interaction from this spec alone.

---

## Table of Contents

1. [Color Palette](#1-color-palette)
2. [Typography Scale](#2-typography-scale)
3. [Responsive Breakpoints](#3-responsive-breakpoints)
4. [Navigation Structure](#4-navigation-structure)
5. [Component List](#5-component-list)
6. [Page Specifications — Public Website](#6-page-specifications--public-website)
7. [Page Specifications — Dashboard](#7-page-specifications--dashboard)
8. [API Contract Summary](#8-api-contract-summary)

---

## 1. Color Palette

Dark theme by default. All colors defined as CSS custom properties on `:root`.

### Backgrounds

| Token              | Hex       | Usage                                      |
|--------------------|-----------|---------------------------------------------|
| `--bg-primary`     | `#0a0a0a` | Page background                             |
| `--bg-secondary`   | `#141414` | Card backgrounds, elevated surfaces         |
| `--bg-tertiary`    | `#1a1a1a` | Input fields, code blocks, hover states     |
| `--bg-hover`       | `#222222` | Interactive element hover                   |
| `--bg-active`      | `#2a2a2a` | Active/pressed states                       |

### Text

| Token              | Hex       | Usage                                       |
|--------------------|-----------|----------------------------------------------|
| `--text-primary`   | `#f5f5f5` | Headlines, body text                         |
| `--text-secondary` | `#a0a0a0` | Bylines, dates, captions, metadata           |
| `--text-tertiary`  | `#666666` | Placeholder text, disabled labels            |
| `--text-inverse`   | `#0a0a0a` | Text on accent-colored backgrounds           |

### Borders

| Token              | Hex       | Usage                                        |
|--------------------|-----------|-----------------------------------------------|
| `--border-primary` | `#222222` | Card borders, dividers                        |
| `--border-secondary`| `#333333`| Input borders, focused card borders           |
| `--border-focus`   | `#6366f1` | Focused input borders                         |

### Accent / Brand

| Token              | Hex       | Usage                                         |
|--------------------|-----------|------------------------------------------------|
| `--accent-primary` | `#6366f1` | Primary CTA buttons, links, active nav items   |
| `--accent-hover`   | `#818cf8` | Button hover, link hover                       |
| `--accent-muted`   | `#6366f120`| Badge backgrounds, tag backgrounds             |

### Category Colors

Each category has a unique color for tags and accents:

| Category     | Hex       |
|-------------|-----------|
| AI          | `#8b5cf6` |
| Programming | `#3b82f6` |
| Startups    | `#10b981` |
| Hardware    | `#f59e0b` |
| Science     | `#ec4899` |
| Industry    | `#6366f1` |

### Semantic Colors

| Token              | Hex       | Usage                                     |
|--------------------|-----------|------------------------------------------|
| `--success`        | `#22c55e` | Published status badge, success toast     |
| `--warning`        | `#f59e0b` | Scheduled status badge, warning toast     |
| `--error`          | `#ef4444` | Error messages, delete buttons            |
| `--info`           | `#3b82f6` | Draft status badge, info toast            |

---

## 2. Typography Scale

Font family: `Inter` loaded via `next/font/google`, fallback: `system-ui, -apple-system, sans-serif`.

Monospace (code blocks): `"JetBrains Mono", "Fira Code", ui-monospace, monospace`.

### Scale

| Token    | Size (rem) | Size (px) | Weight  | Line Height | Letter Spacing | Usage                        |
|----------|-----------|-----------|---------|-------------|----------------|-------------------------------|
| `h1`     | 2.5       | 40        | 800     | 1.1         | -0.03em        | Hero article title             |
| `h2`     | 1.875     | 30        | 700     | 1.2         | -0.02em        | Section headings               |
| `h3`     | 1.25      | 20        | 700     | 1.3         | -0.01em        | Card titles, sidebar headings  |
| `h4`     | 1.0       | 16        | 600     | 1.4         | 0              | Sub-headings                   |
| `body-lg`| 1.125     | 18        | 400     | 1.75        | 0              | Article body text              |
| `body`   | 0.9375    | 15        | 400     | 1.6         | 0              | Default body, card excerpts    |
| `body-sm`| 0.8125    | 13        | 400     | 1.5         | 0              | Metadata, bylines, dates       |
| `caption`| 0.75      | 12        | 500     | 1.4         | 0.02em         | Category tags, badges          |
| `overline`| 0.6875   | 11        | 600     | 1.3         | 0.08em         | Section labels (uppercase)     |

---

## 3. Responsive Breakpoints

Mobile-first approach. Base styles target mobile; breakpoints scale up.

| Name   | Min Width | Columns | Container Max Width | Side Padding |
|--------|-----------|---------|---------------------|--------------|
| `sm`   | 640px     | 1       | 100%                | 16px         |
| `md`   | 768px     | 2       | 100%                | 24px         |
| `lg`   | 1024px    | 3       | 1024px              | 32px         |
| `xl`   | 1280px    | 3       | 1200px              | 40px         |

Content max-width for article body text: `720px` (centered within container).

Grid gap: `24px` at all breakpoints.

---

## 4. Navigation Structure

### 4.1 Public Website — Top Navigation Bar

Sticky, full-width, `height: 64px`, `bg: --bg-secondary`, `border-bottom: 1px solid --border-primary`.

**Layout (left to right):**

| Position | Element           | Details                                                     |
|----------|-------------------|-------------------------------------------------------------|
| Left     | Logo              | "TechNews" wordmark, `h4` weight, white, links to `/`       |
| Center   | Category Links    | `AI` · `Programming` · `Startups` · `Hardware` · `Science` · `Industry` |
| Right    | Search icon button| Opens search overlay                                         |

**Mobile (< 768px):** Category links collapse into a hamburger menu. Menu opens a full-screen overlay with vertical category list and search bar at top.

Each category link uses its category color as an underline on active/hover state (`2px bottom border`).

### 4.2 Public Website — Footer

Full-width, `bg: --bg-secondary`, `padding: 48px 0`.

**3-column grid (single column on mobile):**

| Column       | Content                                                     |
|--------------|-------------------------------------------------------------|
| Brand        | "TechNews" logo, one-line description: "AI & Tech News, Daily." |
| Links        | About, Contact, RSS Feed, Privacy Policy                     |
| Social       | Twitter/X, LinkedIn, GitHub icons (external links)           |

Bottom bar: `border-top: 1px solid --border-primary`, copyright text centered, `body-sm`, `--text-tertiary`.

### 4.3 Dashboard — Sidebar Navigation

Fixed left sidebar, `width: 240px`, `bg: --bg-secondary`, full viewport height.

**Sidebar items (top to bottom):**

| Icon         | Label       | URL                | Badge              |
|-------------|-------------|---------------------|---------------------|
| FileText    | Articles    | `/articles`         | Count of drafts     |
| FolderOpen  | Categories  | `/categories`       | —                   |
| Image       | Media       | `/media`            | —                   |
| Settings    | Settings    | `/settings`         | —                   |

**Bottom of sidebar:**
- User avatar (32px circle) + name + "Editor" or "Admin" role label
- Logout button

**Mobile (< 1024px):** Sidebar collapses to icon-only (`width: 64px`). Labels hidden. Hamburger toggle to expand.

---

## 5. Component List

### 5.1 Public Website Components

#### `<Navbar />`
- Sticky top bar as described in section 4.1
- Props: `categories: Category[]`

#### `<Footer />`
- As described in section 4.2

#### `<HeroArticle />`
- Full-width section, `min-height: 480px`
- Background: featured image with dark gradient overlay (`linear-gradient(to top, #0a0a0a 0%, transparent 60%)`)
- Content (bottom-left, over gradient): category tag, `h1` title, excerpt (`body`, `--text-secondary`, max 2 lines), author avatar + name + date, "Read article" link
- Image: `object-fit: cover`, `aspect-ratio: 16/9` on mobile, full bleed on desktop
- Clicking anywhere navigates to the article

#### `<ArticleCard />`
- `bg: --bg-secondary`, `border: 1px solid --border-primary`, `border-radius: 12px`
- Image thumbnail: top of card, `aspect-ratio: 16/9`, `border-radius: 12px 12px 0 0`
- Content padding: `20px`
- Category tag (top, colored pill)
- Title: `h3`, max 3 lines, ellipsis overflow
- Excerpt: `body`, `--text-secondary`, max 2 lines, ellipsis overflow
- Bottom row: author name (`body-sm`) · date (`body-sm`) · read time (`body-sm`), separated by `·`
- Hover: `border-color: --border-secondary`, slight `translateY(-2px)` with `transition: 150ms ease`
- Entire card is a clickable link to `/article/[slug]`

#### `<ArticleCardCompact />`
- Horizontal layout: thumbnail left (`80px × 80px`, `border-radius: 8px`), text right
- Title: `h4`, max 2 lines
- Bottom: author · date, `body-sm`, `--text-secondary`
- Used in "Trending" sidebar and "Related Articles"

#### `<CategoryTag />`
- Pill shape: `padding: 4px 10px`, `border-radius: 9999px`
- Background: category color at 15% opacity
- Text: category color, `caption` size, uppercase
- Props: `name: string`, `color: string`, `href: string`

#### `<NewsletterSignup />`
- Section with `bg: --bg-secondary`, `border-radius: 16px`, `padding: 48px`
- Heading: `h2` — "Stay in the loop"
- Subtext: `body`, `--text-secondary` — "Get the latest AI & tech news delivered to your inbox every weekday."
- Input + button row: email `<input>` (full width on mobile, 400px on desktop) + "Subscribe" button (`--accent-primary` bg)
- Validation: inline error message for invalid email

#### `<SectionHeading />`
- `overline` text, uppercase, `--text-secondary`
- Optional: right-aligned "View all" link
- Bottom border: `1px solid --border-primary`

#### `<SearchOverlay />`
- Full-screen overlay, `bg: #0a0a0acc` (backdrop blur `12px`)
- Centered search input: large, `h3` size, no border, white text, autofocused
- Results appear below as a list of `<ArticleCardCompact />` (max 5)
- Close with `Escape` key or X button

#### `<ShareButtons />`
- Horizontal row of icon buttons: Twitter/X, LinkedIn, Copy Link
- Copy Link shows "Copied!" toast on click
- Each button: `40px × 40px`, `border-radius: 8px`, `bg: --bg-tertiary`, hover `bg: --bg-hover`

#### `<AuthorBio />`
- Horizontal card: avatar left (48px circle), name + bio right
- Name: `h4`; bio: `body-sm`, `--text-secondary`, max 2 lines

#### `<Pagination />`
- "Previous" and "Next" buttons + page number display
- Buttons: `bg: --bg-tertiary`, `border-radius: 8px`, disabled state at `opacity: 0.4`

#### `<ReadTimeEstimate />`
- Inline text: "X min read", `body-sm`, `--text-secondary`
- Calculated as: `Math.ceil(wordCount / 200)` minutes

### 5.2 Dashboard Components (shadcn/ui based)

All dashboard components use shadcn/ui primitives with the dark theme configured.

#### `<DashboardLayout />`
- Sidebar (section 4.3) + main content area
- Main area: `padding: 32px`, `max-width: 1200px`
- Top bar within main area: page title (`h2`) + primary action button (right-aligned)

#### `<ArticlesTable />`
- shadcn `<Table>` component
- Columns detailed in section 7.2

#### `<ArticleForm />`
- Two-column layout on desktop (main content left 2/3, sidebar right 1/3)
- Fields detailed in section 7.3

#### `<CategoryForm />`
- Single column, simple form
- Fields detailed in section 7.4

#### `<MediaGrid />`
- Grid of image thumbnails, `3 columns` on desktop, `2 on tablet`, `1 on mobile`
- Each item: thumbnail, filename, file size, upload date, delete button (with confirmation dialog)

#### `<LoginForm />`
- Centered card, `max-width: 400px`
- Fields: email input, password input, "Sign in" button
- Error message area below button

#### `<StatusBadge />`
- `published` → green (`--success`), "Published"
- `draft` → blue (`--info`), "Draft"
- `scheduled` → yellow (`--warning`), "Scheduled"
- Uses shadcn `<Badge>` component

#### `<ConfirmDialog />`
- shadcn `<AlertDialog>` with destructive variant
- Props: `title`, `description`, `onConfirm`, `onCancel`

#### `<ImageUploader />`
- Drag-and-drop zone: dashed border, `--border-secondary`, `border-radius: 12px`
- Shows preview after upload
- Accepts: `.jpg`, `.png`, `.webp`, `.gif`
- Max size: `5MB`

---

## 6. Page Specifications — Public Website

### 6.1 Homepage — `/`

**URL:** `/`

**Data needed:** Featured article (latest published), latest articles (paginated, 9 per page), trending articles (top 5 by view count).

**Layout (top to bottom):**

```
┌─────────────────────────────────────────────┐
│                   Navbar                     │
├─────────────────────────────────────────────┤
│                                              │
│              <HeroArticle />                 │
│         (latest featured article)            │
│                                              │
├─────────────────────────────────────────────┤
│                                              │
│  <SectionHeading title="Latest News" />      │
│                                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│  │ Article  │ │ Article  │ │ Article  │     │
│  │ Card     │ │ Card     │ │ Card     │     │
│  └──────────┘ └──────────┘ └──────────┘     │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│  │ Article  │ │ Article  │ │ Article  │     │
│  └──────────┘ └──────────┘ └──────────┘     │
│                                              │
│  ┌──────────────────┐  ┌─────────────────┐  │
│  │  3 more cards    │  │  Trending       │  │
│  │  (continued)     │  │  Sidebar        │  │
│  │                  │  │  5x Compact     │  │
│  │                  │  │  ArticleCards   │  │
│  └──────────────────┘  └─────────────────┘  │
│                                              │
├─────────────────────────────────────────────┤
│           <NewsletterSignup />               │
├─────────────────────────────────────────────┤
│                   Footer                     │
└─────────────────────────────────────────────┘
```

**Detailed section behavior:**

1. **Hero section:** The single most recent published article with `featured_image`. Full viewport width. On mobile, image takes top half, content overlays bottom half.

2. **Latest News grid:**
   - First 6 articles: 3-column grid (`lg+`), 2-column (`md`), 1-column (`sm`)
   - Next 3 articles: 2-column grid left + Trending sidebar right (sidebar takes 1/3 width on `lg+`)
   - On mobile: trending sidebar moves below the final 3 article cards

3. **Trending sidebar:**
   - `<SectionHeading title="Trending" />`
   - 5 `<ArticleCardCompact />` items, numbered 1-5 with large number indicator (24px, `--text-tertiary`, `font-weight: 800`)
   - Sticky within its container on desktop (`position: sticky`, `top: 88px`)

4. **Newsletter signup:** Full-width section, centered content.

5. **Footer:** As described in section 4.2.

**"Load more" behavior:** "Load more" button below the grid. Loads next 9 articles. Button: `bg: --bg-tertiary`, full container width, `padding: 12px`, centered text.

---

### 6.2 Article Page — `/article/[slug]`

**URL:** `/article/[slug]`

**Data needed:** Single article (by slug), related articles (same category, limit 4).

**Layout (top to bottom):**

```
┌─────────────────────────────────────────────┐
│                   Navbar                     │
├─────────────────────────────────────────────┤
│                                              │
│  <CategoryTag />                             │
│                                              │
│  Article Title (h1)                          │
│  Subtitle/Excerpt (body-lg, secondary)       │
│                                              │
│  ┌──────────────────────────────────┐        │
│  │ Author avatar │ Author Name      │        │
│  │               │ Date · Read time │        │
│  └──────────────────────────────────┘        │
│                                              │
│  ┌──────────────────────────────────────┐    │
│  │        Featured Image                │    │
│  │        (full container width)        │    │
│  │        aspect-ratio: 16/9            │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  ┌─────────────── 720px ───────────────┐     │
│  │                                      │    │
│  │   Article Body (rendered markdown)   │    │
│  │                                      │    │
│  │   - Paragraphs: body-lg             │    │
│  │   - H2: h2, margin-top: 48px       │    │
│  │   - H3: h3, margin-top: 32px       │    │
│  │   - Code: bg-tertiary, monospace    │    │
│  │   - Images: full width, rounded     │    │
│  │   - Blockquotes: left border accent │    │
│  │   - Lists: styled bullets/numbers   │    │
│  │                                      │    │
│  └──────────────────────────────────────┘    │
│                                              │
│  <ShareButtons />                            │
│                                              │
│  ─────────── Divider ───────────             │
│                                              │
│  <AuthorBio />                               │
│                                              │
│  ─────────── Divider ───────────             │
│                                              │
│  <SectionHeading title="Related Articles" /> │
│                                              │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐       │
│  │ Card │ │ Card │ │ Card │ │ Card │       │
│  └──────┘ └──────┘ └──────┘ └──────┘       │
│                                              │
├─────────────────────────────────────────────┤
│                   Footer                     │
└─────────────────────────────────────────────┘
```

**Article body markdown rendering rules:**

| Element       | Style                                                              |
|---------------|--------------------------------------------------------------------|
| `<p>`         | `body-lg`, `--text-primary`, `margin-bottom: 24px`                 |
| `<h2>`        | `h2`, `margin-top: 48px`, `margin-bottom: 16px`                   |
| `<h3>`        | `h3`, `margin-top: 32px`, `margin-bottom: 12px`                   |
| `<a>`         | `--accent-primary`, underline on hover                             |
| `<code>`      | Inline: `bg: --bg-tertiary`, `padding: 2px 6px`, `border-radius: 4px` |
| `<pre><code>` | `bg: --bg-tertiary`, `padding: 20px`, `border-radius: 8px`, horizontal scroll |
| `<blockquote>`| `border-left: 3px solid --accent-primary`, `padding-left: 20px`, italic |
| `<img>`       | `width: 100%`, `border-radius: 8px`, `margin: 32px 0`             |
| `<ul>/<ol>`   | `padding-left: 24px`, `margin-bottom: 24px`, `body-lg`            |
| `<hr>`        | `border-top: 1px solid --border-primary`, `margin: 40px 0`        |

**Related articles:** 4-column grid on `lg+`, 2-column on `md`, 1-column on `sm`. Uses `<ArticleCard />`.

**SEO:** Page uses `meta_title` and `meta_description` from article data. Open Graph tags with featured image.

---

### 6.3 Category Page — `/category/[slug]`

**URL:** `/category/[slug]`

**Data needed:** Category details, articles filtered by category (paginated, 12 per page).

**Layout:**

```
┌─────────────────────────────────────────────┐
│                   Navbar                     │
├─────────────────────────────────────────────┤
│                                              │
│  Category Name (h1, colored with             │
│  category color)                             │
│  Category description (body, secondary)      │
│                                              │
│  Article count: "42 articles"                │
│                                              │
├─────────────────────────────────────────────┤
│                                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│  │ Article  │ │ Article  │ │ Article  │     │
│  │ Card     │ │ Card     │ │ Card     │     │
│  └──────────┘ └──────────┘ └──────────┘     │
│  ... (12 per page, 3-col grid)               │
│                                              │
│  <Pagination />                              │
│                                              │
├─────────────────────────────────────────────┤
│                   Footer                     │
└─────────────────────────────────────────────┘
```

Grid: 3-column on `lg+`, 2-column on `md`, 1-column on `sm`. Uses `<ArticleCard />`.

**Pagination:** Bottom center. Shows "Page X of Y". Previous/Next buttons.

---

### 6.4 About Page — `/about`

**URL:** `/about`

Simple static page:

- `h1` — "About TechNews"
- Body text (`body-lg`, max-width `720px`, centered) with mission statement
- No dynamic data

---

### 6.5 Search Results — `/search?q=...`

**URL:** `/search?q={query}`

**Data needed:** Articles matching search query (searches title, excerpt, content).

**Layout:**

```
┌─────────────────────────────────────────────┐
│                   Navbar                     │
├─────────────────────────────────────────────┤
│                                              │
│  Search input (large, prefilled with q)      │
│  Results count: "12 results for 'query'"     │
│                                              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│  │ Article  │ │ Article  │ │ Article  │     │
│  │ Card     │ │ Card     │ │ Card     │     │
│  └──────────┘ └──────────┘ └──────────┘     │
│  ... (same grid as category page)            │
│                                              │
│  <Pagination />                              │
│                                              │
│  (If no results: empty state message         │
│   "No articles found for 'query'")           │
│                                              │
├─────────────────────────────────────────────┤
│                   Footer                     │
└─────────────────────────────────────────────┘
```

Search input: debounced (300ms), updates URL param on submit (Enter or button click). Same article grid as category page.

---

## 7. Page Specifications — Dashboard

All dashboard pages use `<DashboardLayout />` (sidebar + main content area).

### 7.1 Login Page — `/login`

**URL:** `/login` (no sidebar, centered layout)

**Layout:** Centered card, `max-width: 400px`, `bg: --bg-secondary`, `border-radius: 16px`, `padding: 40px`.

**Elements:**

| Element           | Type            | Details                                       |
|-------------------|-----------------|-----------------------------------------------|
| Logo              | Text            | "TechNews" wordmark + "Dashboard" subtitle    |
| Email             | `<Input>`       | `type="email"`, placeholder "Email address"   |
| Password          | `<Input>`       | `type="password"`, placeholder "Password"     |
| Sign in button    | `<Button>`      | Full width, `--accent-primary` bg             |
| Error message     | Text            | Red (`--error`), appears below button on fail |

**Behavior:** POST to `/api/auth/login`. On success, store JWT in `httpOnly` cookie, redirect to `/articles`. On error, show error message (e.g., "Invalid email or password").

---

### 7.2 Articles List — `/articles`

**URL:** `/articles`

**Top bar:**
- Title: "Articles" (`h2`)
- Button: "New Article" → navigates to `/articles/new`

**Filters row (below top bar):**

| Filter      | Type          | Options                                      |
|-------------|---------------|----------------------------------------------|
| Search      | Text input    | Placeholder "Search articles...", debounced   |
| Status      | `<Select>`    | All, Draft, Published, Scheduled              |
| Category    | `<Select>`    | All, + each category by name                  |

**Table columns:**

| Column     | Width   | Content                                                        |
|------------|---------|----------------------------------------------------------------|
| Title      | 40%     | Article title (clickable link to edit page) + slug below in `body-sm --text-tertiary` |
| Status     | 10%     | `<StatusBadge />` (Draft / Published / Scheduled)              |
| Category   | 15%     | `<CategoryTag />`                                              |
| Author     | 15%     | Author name                                                    |
| Date       | 15%     | `published_at` or `created_at`, formatted as "Feb 16, 2026"   |
| Actions    | 5%      | `...` menu → Edit, Delete (with `<ConfirmDialog />`)           |

**Empty state:** "No articles found. Create your first article."

**Pagination:** Below table. 20 articles per page. Previous/Next + page indicator.

---

### 7.3 Article Editor — `/articles/new` and `/articles/[id]/edit`

**URL:** `/articles/new` (create), `/articles/[id]/edit` (edit)

**Top bar:**
- Title: "New Article" or "Edit Article" (`h2`)
- Buttons (right): "Save Draft" (secondary), "Publish" (primary, `--accent-primary`)
- When editing a published article: "Update" replaces "Publish"

**Two-column layout on desktop (stacked on mobile):**

**Main column (left, 2/3 width):**

| Field          | Type          | Details                                                     |
|----------------|---------------|-------------------------------------------------------------|
| Title          | `<Input>`     | Large input, `h3` font size, placeholder "Article title"    |
| Slug           | `<Input>`     | Auto-generated from title (kebab-case), editable, `body-sm`, prefixed with `/article/` label |
| Content        | `<Textarea>`  | Markdown editor. Monospace font. Min-height: `400px`. Full width. |
| Excerpt        | `<Textarea>`  | Max 280 characters, character counter shown. Placeholder "Brief summary of the article..." |

**Sidebar column (right, 1/3 width):**

| Field          | Type                 | Details                                                   |
|----------------|----------------------|-----------------------------------------------------------|
| Status         | `<Select>`           | Draft, Published, Scheduled                               |
| Publish Date   | `<DatePicker>`       | Shown always. Defaults to now for Published.              |
| Category       | `<Select>`           | Dropdown of all categories                                |
| Featured Image | `<ImageUploader />`  | Drag-and-drop or click to upload. Shows preview.          |
| Meta Title     | `<Input>`            | SEO title, placeholder "SEO title (optional)"             |
| Meta Description| `<Textarea>`        | SEO description, max 160 chars, character counter         |

**Behavior:**
- Title changes auto-update slug (only on create, not edit)
- Save Draft: saves with `status: "draft"`, shows success toast
- Publish: saves with `status: "published"`, `published_at: now`, shows success toast, redirects to articles list
- Unsaved changes: browser `beforeunload` confirmation

---

### 7.4 Categories — `/categories`

**URL:** `/categories`

**Top bar:**
- Title: "Categories" (`h2`)
- Button: "New Category" → opens modal form

**Table columns:**

| Column      | Width  | Content                                              |
|-------------|--------|------------------------------------------------------|
| Color       | 5%     | Circle swatch (`16px`, filled with category color)   |
| Name        | 30%    | Category name (clickable to edit)                    |
| Slug        | 25%    | Slug text, `--text-secondary`                        |
| Description | 25%    | Truncated description, max 1 line                    |
| Articles    | 10%    | Count of articles in category                        |
| Actions     | 5%     | `...` menu → Edit, Delete (with `<ConfirmDialog />`) |

**Category form (modal dialog):**

| Field       | Type          | Details                                               |
|-------------|---------------|-------------------------------------------------------|
| Name        | `<Input>`     | Category name, required                               |
| Slug        | `<Input>`     | Auto-generated from name, editable                    |
| Description | `<Textarea>`  | Short description                                     |
| Color       | Color picker  | Preset palette of 8 colors + hex input                |

---

### 7.5 Media Library — `/media`

**URL:** `/media`

**Top bar:**
- Title: "Media" (`h2`)
- Button: "Upload" → opens file picker (multi-select)

**Upload area:** Drag-and-drop zone spanning full width when no media exists. Otherwise, "Upload" button in top bar.

**Grid:** Each item is a card:
- Thumbnail: `aspect-ratio: 1`, `object-fit: cover`, `border-radius: 8px`
- Below thumbnail: filename (truncated, `body-sm`), file size (`body-sm`, `--text-secondary`)
- Hover overlay: delete icon button (top-right corner)
- Click: opens modal with full-size preview + metadata (filename, dimensions, size, upload date, URL with copy button)

3 columns on `lg+`, 2 on `md`, 1 on `sm`.

---

### 7.6 Settings — `/settings`

**URL:** `/settings`

**Top bar:**
- Title: "Settings" (`h2`)
- Button: "Save Changes" (primary)

**Form sections (stacked):**

**General:**

| Field          | Type          | Details                                    |
|----------------|---------------|--------------------------------------------|
| Site Name      | `<Input>`     | Default: "TechNews"                        |
| Site Description| `<Textarea>` | One-liner description                      |

**Social Links:**

| Field     | Type      | Details                                     |
|-----------|-----------|---------------------------------------------|
| Twitter/X | `<Input>` | Full URL, placeholder "https://x.com/..."   |
| LinkedIn  | `<Input>` | Full URL                                    |
| GitHub    | `<Input>` | Full URL                                    |

**Newsletter:**

| Field              | Type       | Details                                     |
|--------------------|------------|---------------------------------------------|
| Newsletter enabled | `<Switch>` | Toggle on/off                               |
| Provider           | `<Select>` | None, Mailchimp, ConvertKit, Custom         |
| Webhook URL        | `<Input>`  | Shown when provider is not "None"           |

---

## 8. API Contract Summary

All endpoints return JSON. Public endpoints require no auth. Dashboard endpoints require `Authorization: Bearer <jwt>` header.

### Public Endpoints

| Method | URL                          | Query Params                                    | Response                                                 |
|--------|------------------------------|-------------------------------------------------|----------------------------------------------------------|
| GET    | `/api/articles`              | `page` (int, default 1), `limit` (int, default 12), `category` (slug), `search` (string) | `{ articles: Article[], total: int, page: int, totalPages: int }` |
| GET    | `/api/articles/:slug`        | —                                               | `{ article: Article }` (includes author, category)       |
| GET    | `/api/articles/trending`     | `limit` (int, default 5)                        | `{ articles: Article[] }`                                |
| GET    | `/api/categories`            | —                                               | `{ categories: Category[] }`                             |

### Dashboard Endpoints (auth required)

| Method | URL                          | Body / Query                                    | Response                                                 |
|--------|------------------------------|-------------------------------------------------|----------------------------------------------------------|
| POST   | `/api/auth/login`            | `{ email, password }`                           | `{ token: string, user: User }`                          |
| GET    | `/api/dashboard/articles`    | `page`, `limit`, `status`, `category`, `search` | `{ articles: Article[], total, page, totalPages }`       |
| GET    | `/api/dashboard/articles/:id`| —                                               | `{ article: Article }`                                   |
| POST   | `/api/dashboard/articles`    | Article body (JSON)                             | `{ article: Article }`                                   |
| PUT    | `/api/dashboard/articles/:id`| Article body (JSON)                             | `{ article: Article }`                                   |
| DELETE | `/api/dashboard/articles/:id`| —                                               | `{ success: true }`                                      |
| GET    | `/api/dashboard/categories`  | —                                               | `{ categories: Category[] }`                             |
| POST   | `/api/dashboard/categories`  | `{ name, slug, description, color }`            | `{ category: Category }`                                 |
| PUT    | `/api/dashboard/categories/:id`| `{ name, slug, description, color }`          | `{ category: Category }`                                 |
| DELETE | `/api/dashboard/categories/:id`| —                                             | `{ success: true }`                                      |
| GET    | `/api/dashboard/media`       | —                                               | `{ media: Media[] }`                                     |
| POST   | `/api/dashboard/media/upload`| `multipart/form-data` (file field)              | `{ media: Media }`                                       |
| DELETE | `/api/dashboard/media/:id`   | —                                               | `{ success: true }`                                      |
| GET    | `/api/dashboard/settings`    | —                                               | `{ settings: Settings }`                                 |
| PUT    | `/api/dashboard/settings`    | Settings body (JSON)                            | `{ settings: Settings }`                                 |

---

## Appendix: Data Shapes

```typescript
interface Article {
  id: number;
  title: string;
  slug: string;
  excerpt: string;
  content: string; // markdown
  featured_image: string | null;
  category_id: number;
  author_id: number;
  status: "draft" | "published" | "scheduled";
  published_at: string | null; // ISO 8601
  meta_title: string | null;
  meta_description: string | null;
  created_at: string; // ISO 8601
  updated_at: string; // ISO 8601
  // Joined fields (when expanded):
  author?: Author;
  category?: Category;
}

interface Category {
  id: number;
  name: string;
  slug: string;
  description: string;
  color: string; // hex
}

interface Author {
  id: number;
  name: string;
  email: string;
  avatar: string | null;
  bio: string | null;
  role: "admin" | "editor";
}

interface Media {
  id: number;
  filename: string;
  url: string;
  mime_type: string;
  size: number; // bytes
  uploaded_at: string; // ISO 8601
}

interface Settings {
  site_name: string;
  site_description: string;
  social_twitter: string | null;
  social_linkedin: string | null;
  social_github: string | null;
  newsletter_enabled: boolean;
  newsletter_provider: "none" | "mailchimp" | "convertkit" | "custom";
  newsletter_webhook_url: string | null;
}
```
