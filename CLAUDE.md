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

## Commands
- `pnpm install` — install deps
- `pnpm dev` — start all apps
- `pnpm build` — build all

## Read BRIEF.md for full product specification
