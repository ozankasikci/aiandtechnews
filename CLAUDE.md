# CLAUDE.md — AI & Tech News Website

## Project Structure
- Monorepo: pnpm workspaces + turborepo
- `apps/web` — Public news website (Next.js 15, App Router, Tailwind CSS)
- `apps/dashboard` — CMS dashboard (Next.js 15, App Router, Tailwind CSS, shadcn/ui)
- `apps/server` — API backend (Node.js, Express, TypeScript, better-sqlite3)
- `packages/shared` — Shared types and utilities

## Tech Stack
- Next.js 15 with App Router
- TypeScript strict mode
- Tailwind CSS v4
- shadcn/ui (dashboard only)
- better-sqlite3 for database
- Express.js for API
- JWT auth for dashboard

## Design
- Dark theme by default
- Clean, content-first design inspired by The Verge, TechCrunch, Ars Technica
- Mobile-first responsive
- Good typography (Inter or system fonts)

## Ports
- Web: 3000
- Dashboard: 3001
- Server: 4001

## Key Docs
- `BRIEF.md` — Full product brief
- `PRODUCT_SPEC.md` — Detailed spec (colors, typography, components, layouts)
- `NEWS_PUBLISHING_POLICY.md` - Mandatory source, writing, attribution, image, and automation rules for every manual or automated article

Read `NEWS_PUBLISHING_POLICY.md` before any news selection, writing, importing, or publishing work. It is the repository source of truth for article operations.

## Commands
- `pnpm install` — install deps
- `pnpm dev` — start all apps
- `pnpm build` — build all

## Pending Refactor
- Server needs modular structure: `modules/news/`, `modules/categories/`, `modules/auth/`, `modules/media/`
- Each module: routes, service, model files
