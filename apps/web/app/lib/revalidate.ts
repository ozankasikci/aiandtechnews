import { ARTICLES_TAG, articleTag } from "./api";
import { isAuthorizedCronRequest } from "./indexnow";
import { TOPICS_TAG, topicTag } from "./topics";

// POST /api/revalidate lets the API refresh cached pages the moment an
// article is published, illustrated, edited, or deleted, instead of waiting
// for the ISR timers (an hour for article pages, five minutes for lists).

export const MAX_REVALIDATE_SLUGS = 50;
// Generated slugs are lowercase letters, digits, and hyphens (Go
// content.Slugify); a dashboard edit may set others, so any URL-unreserved
// characters are accepted, starting with a letter or digit (never "." or
// ".."), up to 200 characters.
const SLUG_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._~-]*$/;
const MAX_SLUG_LENGTH = 200;

export function isValidSlug(value: unknown): value is string {
  return typeof value === "string" && value.length <= MAX_SLUG_LENGTH && SLUG_PATTERN.test(value);
}

export type ParsedRevalidateBody = { ok: true; slugs: string[]; topics: string[] } | { ok: false; error: string };

export function parseRevalidateBody(body: unknown): ParsedRevalidateBody {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return { ok: false, error: "Body must be a JSON object with a slugs array" };
  }
  const slugs = (body as { slugs?: unknown }).slugs;
  if (!Array.isArray(slugs)) return { ok: false, error: "slugs must be an array" };
  if (slugs.length > MAX_REVALIDATE_SLUGS) {
    return { ok: false, error: `At most ${MAX_REVALIDATE_SLUGS} slugs per request` };
  }
  if (!slugs.every(isValidSlug)) return { ok: false, error: "Every slug must be a valid article slug" };

  // Optional: topic hubs the change touched.
  const rawTopics = (body as { topics?: unknown }).topics;
  let topics: string[] = [];
  if (rawTopics !== undefined && rawTopics !== null) {
    if (!Array.isArray(rawTopics)) return { ok: false, error: "topics must be an array" };
    if (rawTopics.length > MAX_REVALIDATE_SLUGS) {
      return { ok: false, error: `At most ${MAX_REVALIDATE_SLUGS} topics per request` };
    }
    if (!rawTopics.every(isValidSlug)) return { ok: false, error: "Every topic must be a valid topic slug" };
    topics = [...new Set(rawTopics)];
  }
  return { ok: true, slugs: [...new Set(slugs)], topics };
}

export interface RevalidateTargets {
  tags: string[];
  paths: { path: string; type?: "page" | "layout" }[];
}

// Everything a changed article can appear on: its own page (and its data
// fetch, whose tag also clears a not-found render for a slug that was just
// published), plus every list: homepage, category pages, archive, feeds,
// sitemaps, and the homepage share card.
export function revalidateTargets(slugs: string[], topics: string[] = []): RevalidateTargets {
  return {
    tags: [...slugs.map(articleTag), ARTICLES_TAG, ...topics.map(topicTag), ...(topics.length ? [TOPICS_TAG] : [])],
    paths: [
      ...slugs.map((slug) => ({ path: `/article/${slug}` })),
      ...topics.map((slug) => ({ path: `/topics/${slug}` })),
      ...(topics.length ? [{ path: "/topics" }] : []),
      { path: "/" },
      { path: "/archive" },
      { path: "/[category]", type: "page" },
      { path: "/sitemap.xml" },
      { path: "/news-sitemap.xml" },
      { path: "/rss.xml" },
      { path: "/og/today" },
    ],
  };
}

export interface RevalidateDeps {
  secret: string | undefined;
  revalidateTag: (tag: string) => void;
  revalidatePath: (path: string, type?: "page" | "layout") => void;
}

export async function handleRevalidateRequest(request: Request, deps: RevalidateDeps): Promise<Response> {
  if (!isAuthorizedCronRequest(request.headers.get("authorization"), deps.secret)) {
    return Response.json({ error: "Unauthorized" }, { status: 401 });
  }
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return Response.json({ error: "Invalid JSON body" }, { status: 400 });
  }
  const parsed = parseRevalidateBody(body);
  if (!parsed.ok) return Response.json({ error: parsed.error }, { status: 400 });

  const targets = revalidateTargets(parsed.slugs, parsed.topics);
  for (const tag of targets.tags) deps.revalidateTag(tag);
  for (const { path, type } of targets.paths) {
    if (type) deps.revalidatePath(path, type);
    else deps.revalidatePath(path);
  }
  return Response.json({
    revalidated: true,
    slugs: parsed.slugs,
    topics: parsed.topics,
    tags: targets.tags,
    paths: targets.paths.map(({ path }) => path),
  });
}
