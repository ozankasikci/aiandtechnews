const DEFAULT_ENDPOINT = "https://api.indexnow.org/indexnow";
const DEFAULT_SITE_ORIGIN = "https://www.aiandtech.news";

export const INDEXNOW_KEY = "841819f6d7012eca7cd9104b4dd45d8e";

type FetchImplementation = typeof fetch;

export interface IndexNowOptions {
  endpoint?: string;
  fetchImplementation?: FetchImplementation;
  siteOrigin?: string;
}

export interface IndexNowResult {
  status: number;
  submitted: number;
}

export function articleUrl(slug: string, siteOrigin = DEFAULT_SITE_ORIGIN): string {
  const normalizedOrigin = new URL(siteOrigin).origin;
  return new URL(`/article/${encodeURIComponent(slug)}`, normalizedOrigin).toString();
}

function prepareUrls(urls: string[], siteOrigin: string): string[] {
  const origin = new URL(siteOrigin).origin;
  const uniqueUrls = [...new Set(urls)];

  if (uniqueUrls.length > 10_000) {
    throw new Error("IndexNow accepts at most 10,000 URLs per request");
  }

  for (const url of uniqueUrls) {
    if (new URL(url).origin !== origin) {
      throw new Error(`IndexNow URL does not belong to ${origin}: ${url}`);
    }
  }

  return uniqueUrls;
}

export async function submitIndexNowUrls(
  urls: string[],
  options: IndexNowOptions = {},
): Promise<IndexNowResult> {
  const siteOrigin = new URL(options.siteOrigin || DEFAULT_SITE_ORIGIN).origin;
  const urlList = prepareUrls(urls, siteOrigin);
  if (urlList.length === 0) return { status: 204, submitted: 0 };

  const response = await (options.fetchImplementation || fetch)(options.endpoint || DEFAULT_ENDPOINT, {
    method: "POST",
    headers: { "Content-Type": "application/json; charset=utf-8" },
    body: JSON.stringify({
      host: new URL(siteOrigin).host,
      key: INDEXNOW_KEY,
      keyLocation: `${siteOrigin}/${INDEXNOW_KEY}.txt`,
      urlList,
    }),
  });

  if (response.status !== 200 && response.status !== 202) {
    const detail = (await response.text()).trim().slice(0, 300);
    throw new Error(`IndexNow rejected ${urlList.length} URL(s) with ${response.status}${detail ? `: ${detail}` : ""}`);
  }

  return { status: response.status, submitted: urlList.length };
}

export function submitArticleSlugsToIndexNow(
  slugs: string[],
  options: IndexNowOptions = {},
): Promise<IndexNowResult> {
  const siteOrigin = options.siteOrigin || DEFAULT_SITE_ORIGIN;
  return submitIndexNowUrls(slugs.map((slug) => articleUrl(slug, siteOrigin)), options);
}
