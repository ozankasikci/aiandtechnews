# TechNews — AI & Tech News Website

## Overview
A modern AI and technology news website with a content management dashboard. Daily news publishing about AI, programming, startups, and tech industry.

## Architecture
- **Monorepo**: pnpm workspaces + turbo
- **Website** (`apps/web`): Next.js 15, TypeScript, Tailwind CSS — public-facing news site
- **Dashboard** (`apps/dashboard`): Next.js 15, TypeScript, Tailwind CSS, shadcn/ui — CMS for managing articles
- **Server** (`apps/server`): Node.js + Express + TypeScript — API backend
- **Shared** (`packages/shared`): Shared types and utilities
- **Database**: SQLite (simple, no Docker needed)

## Design References (analyze and take inspiration from)
These are the top tech news sites to study for layout/UX patterns:

### The Verge (theverge.com)
- Bold, editorial design with large typography
- Blog/liveblog-style homepage with timestamped entries
- Strong use of color accents and featured story blocks
- Sticky nav with categories
- Dark theme option

### TechCrunch (techcrunch.com)
- Classic news layout: hero story + grid of articles
- Category-based navigation (Startups, AI, Apps, Crypto, etc.)
- Author bylines prominent
- Newsletter signup integration
- Clean card-based article previews

### Ars Technica (arstechnica.com)
- Dense, information-rich layout
- Story list with thumbnails on left, text on right
- Strong category system (Tech, Science, Gaming, Policy)
- Comment counts visible
- Long-form article focus

### The Information (theinformation.com)
- Premium, clean, minimal design
- Focus on exclusive/original reporting
- Simple typography, lots of whitespace
- Newsletter-first approach

### Hacker News-inspired visual reference (news.ycombinator.com)

This reference is visual only. Hacker News must never be used as a news source or discovery feed. See `NEWS_PUBLISHING_POLICY.md`.
- Ultra-minimal, text-only
- Upvote system, comment counts
- Pure link aggregation

## Website Pages

### Homepage (`/`)
- **Hero section**: Featured/latest article with large image
- **Latest news grid**: 2-3 column card grid with thumbnail, title, excerpt, author, date, category tag
- **Sidebar or section**: "Trending" or "Most Read" list
- **Categories quick nav**: AI, Programming, Startups, Hardware, Science, Industry
- **Newsletter signup**: Email capture section
- **Footer**: About, Contact, Social links, RSS

### Article Page (`/article/[slug]`)
- **Article header**: Title, subtitle, author (with avatar), publish date, read time, category
- **Featured image**: Full-width hero image
- **Article body**: Rich text content (markdown rendered)
- **Share buttons**: Twitter/X, LinkedIn, Copy link
- **Related articles**: 3-4 cards at bottom
- **Author bio**: Small card with links

### Category Page (`/category/[slug]`)
- Category name + description header
- Filtered article grid (same layout as homepage grid)
- Pagination

### About Page (`/about`)
- Mission statement, team info

### Search Results (`/search?q=...`)
- Search bar + filtered results

## Dashboard Pages

### Login (`/login`)
- Simple email/password auth

### Articles List (`/articles`)
- Table with: title, status (draft/published/scheduled), category, author, date, actions
- Filters: status, category, date range
- Search
- Bulk actions

### Article Editor (`/articles/new` and `/articles/[id]/edit`)
- Title input
- Slug (auto-generated from title)
- Category selector
- Featured image upload
- Rich markdown editor (or WYSIWYG)
- Excerpt/summary field
- SEO fields (meta title, description)
- Status: Draft / Published / Scheduled
- Publish date picker
- Save draft / Publish buttons
- Preview

### Categories (`/categories`)
- CRUD for categories
- Name, slug, description, color

### Media Library (`/media`)
- Upload and manage images
- Grid view with thumbnails

### Settings (`/settings`)
- Site name, description
- Social links
- Newsletter settings

## Data Model

### Article
- id, title, slug, excerpt, content (markdown), featured_image
- category_id, author_id
- status (draft/published/scheduled), published_at
- meta_title, meta_description
- created_at, updated_at

### Category
- id, name, slug, description, color

### Author
- id, name, email, password_hash, avatar, bio, role (admin/editor)

### Media
- id, filename, url, mime_type, size, uploaded_at

## API Endpoints

### Public
- `GET /api/articles` — list published articles (pagination, category filter, search)
- `GET /api/articles/:slug` — single article
- `GET /api/categories` — list categories
- `GET /api/articles/trending` — most viewed articles

### Dashboard (auth required)
- CRUD for articles, categories, authors, media
- `POST /api/auth/login`
- `POST /api/media/upload`

## Tech Stack Details
- **Next.js 15** with App Router
- **Tailwind CSS v4** 
- **shadcn/ui** for dashboard components
- **better-sqlite3** for database
- **JWT** for dashboard auth
- **sharp** for image optimization
- **gray-matter + remark** for markdown processing
- **Express.js** for API server

## Design Guidelines
- **Clean, modern, content-first** — let the articles breathe
- **Dark theme** by default (dark bg, light text) — matches our other projects
- **Good typography**: System font stack or Inter/Geist
- **Mobile-first responsive design**
- **Fast**: Static generation where possible, ISR for articles
- **Accessible**: Semantic HTML, ARIA labels, keyboard nav
