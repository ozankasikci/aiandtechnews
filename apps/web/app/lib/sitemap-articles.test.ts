import assert from "node:assert/strict";
import test from "node:test";
import { getAllArticlesForSitemap, type ApiArticle, type getArticles } from "./api";

type GetPage = typeof getArticles;

function articles(count: number): ApiArticle[] {
  return Array.from({ length: count }, (_, index) => ({
    id: index + 1,
    slug: `article-${index + 1}`,
  } as ApiArticle));
}

function pagedArticles(all: ApiArticle[]): GetPage {
  return async ({ page = 1, limit = 50 } = {}) => ({
    articles: all.slice((page - 1) * limit, page * limit),
    total: all.length,
    page,
    totalPages: Math.ceil(all.length / limit),
  });
}

test("sitemap article pagination includes more than 500 published articles", async () => {
  const all = articles(525);
  const requestedPages: number[] = [];
  const getPage: GetPage = async (options) => {
    assert.equal(options?.limit, 50);
    requestedPages.push(options?.page ?? 1);
    return pagedArticles(all)(options);
  };

  const result = await getAllArticlesForSitemap(getPage);

  assert.deepEqual(result?.map((article) => article.slug), all.map((article) => article.slug));
  assert.deepEqual(requestedPages, Array.from({ length: 11 }, (_, index) => index + 1));
});

test("sitemap article pagination accepts an empty published archive", async () => {
  assert.deepEqual(await getAllArticlesForSitemap(pagedArticles([])), []);
});

test("sitemap article pagination rejects a failed later page instead of returning a partial list", async () => {
  const getPage: GetPage = async (options) =>
    options?.page === 2 ? null : pagedArticles(articles(75))(options);

  assert.equal(await getAllArticlesForSitemap(getPage), null);
});

test("sitemap article pagination rejects an incomplete page", async () => {
  const getPage: GetPage = async (options) => {
    const data = (await pagedArticles(articles(75))(options))!;
    return options?.page === 2 ? { ...data, articles: [] } : data;
  };

  assert.equal(await getAllArticlesForSitemap(getPage), null);
});

test("sitemap article pagination rejects repeated slugs across page boundaries", async () => {
  const all = articles(75);
  all[50].slug = all[49].slug;

  assert.equal(await getAllArticlesForSitemap(pagedArticles(all)), null);
});
