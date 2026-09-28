import { getArticles, type ApiArticle } from "../lib/api";

export const ARCHIVE_PAGE_SIZE = 24;

export type ArchivePageResult =
  | { state: "found"; page: number; total: number; totalPages: number; articles: ApiArticle[] }
  | { state: "missing" }
  | { state: "unavailable" };

export function parseArchivePage(value: string | string[] | undefined): number | null {
  if (value === undefined) return 1;
  if (typeof value !== "string" || !/^[1-9]\d*$/.test(value)) return null;

  const page = Number(value);
  return Number.isSafeInteger(page) ? page : null;
}

export function archiveHref(page: number): string {
  return page === 1 ? "/archive" : `/archive?page=${page}`;
}

export async function getArchivePage(
  value: string | string[] | undefined,
  getPage: typeof getArticles = getArticles,
): Promise<ArchivePageResult> {
  const page = parseArchivePage(value);
  if (page === null) return { state: "missing" };

  let data: Awaited<ReturnType<typeof getArticles>>;
  try {
    data = await getPage({ page, limit: ARCHIVE_PAGE_SIZE });
  } catch {
    return { state: "unavailable" };
  }

  if (
    !data ||
    !Array.isArray(data.articles) ||
    !Number.isSafeInteger(data.total) ||
    !Number.isSafeInteger(data.page) ||
    !Number.isSafeInteger(data.totalPages) ||
    data.total < 0 ||
    data.page !== page ||
    data.totalPages !== Math.ceil(data.total / ARCHIVE_PAGE_SIZE)
  ) {
    return { state: "unavailable" };
  }

  if (page > Math.max(data.totalPages, 1)) return { state: "missing" };

  const expectedCount = Math.min(ARCHIVE_PAGE_SIZE, data.total - (page - 1) * ARCHIVE_PAGE_SIZE);
  if (data.articles.length !== expectedCount) return { state: "unavailable" };

  return { state: "found", page, total: data.total, totalPages: data.totalPages, articles: data.articles };
}
