# Search indexing audit, 28 September 2026

This is a read-only snapshot of the production site and the `aiandtech.news` domain property in Google Search Console (GSC). The [page indexing report](https://search.google.com/search-console/index?resource_id=sc-domain%3Aaiandtech.news) says its last update was 21 September. GSC data and the live site therefore describe different points in time. No indexing request or validation was submitted during this audit.

## What GSC shows

| Indexing state | URLs | Representative examples |
| --- | ---: | --- |
| Indexed | 230 | GSC total |
| Discovered, currently not indexed | 74 | [Anthropic legal AI story](https://www.aiandtech.news/article/anthropic-enters-legal-ai-race), newsletter editions |
| Crawled, currently not indexed | 10 | [GitLab Duo story](https://www.aiandtech.news/article/gitlab-duo-expands-self-hosted-ai-options-through-microsoft-foundry), `/rss.xml`, `http://aiandtech.news/` |
| Page with redirect | 1 | `https://aiandtech.news/` |
| Not found (404) | 1 | [AI startup funding story](https://www.aiandtech.news/article/17-us-ai-startups-100m-rounds-2026), last crawled 18 July |

The 74 discovered examples include article URLs and 20 dated newsletter archive URLs. GSC shows no last crawl for those examples. The 10 crawled examples include seven article pages, one newsletter edition, RSS, and the old HTTP homepage. These groups should not be treated as 84 broken article pages.

Both submitted sitemaps show **Success** in [GSC Sitemaps](https://search.google.com/search-console/sitemaps?resource_id=sc-domain%3Aaiandtech.news), last read 27 September. GSC reported 464 discovered URLs in the regular sitemap and 35 in the news sitemap at that read. On 28 September, the live [regular sitemap](https://www.aiandtech.news/sitemap.xml) returned HTTP 200 with 486 URL entries, including 400 article URLs and 29 newsletter editions; the [news sitemap](https://www.aiandtech.news/news-sitemap.xml) returned HTTP 200 with 44 entries. Different counts across those dates are expected as new pages arrive.

## Live checks and limits of the diagnosis

- The sampled [discovered article](https://www.aiandtech.news/article/anthropic-enters-legal-ai-race), [crawled article](https://www.aiandtech.news/article/gitlab-duo-expands-self-hosted-ai-options-through-microsoft-foundry), and [newsletter edition](https://www.aiandtech.news/newsletter/archive/2026-09-14) returned HTTP 200 on 28 September. Their HTML included an index/follow robots tag, a self-referencing canonical URL, a title, a description, and an H1. The two article slugs and newsletter edition were present in the regular sitemap.
- GSC URL Inspection for the Anthropic legal AI article reported **“URL is unknown to Google”**, with no recorded crawl or referring sitemap. Its 28 September live test reported **“URL is available to Google”**: smartphone fetch successful, crawling allowed, indexing allowed, and the expected user-declared canonical. This confirms current fetchability for that sample, but a successful live test does not promise indexing.
- The URL in GSC's 404 example now returns HTTP 200 with a self-canonical, indexable article page. The 404 record reflects an older crawl and should be checked again after GSC refreshes. It does not justify adding a redirect or republishing the article today.
- The bare HTTPS domain redirects to `https://www.aiandtech.news/` with HTTP 307. The HTTP domain redirects to HTTPS with HTTP 308, then reaches the `www` URL. The redirected URL is an expected nonindexed duplicate. `/rss.xml` returns HTTP 200 as XML and does not need to become a search result.

The code at the start of this sprint offers two plausible discovery improvements, without proving either caused these GSC statuses. [The regular sitemap](../apps/web/app/sitemap.ts) stamped unchanged static, category, and glossary pages with the current time; it also stopped after 500 articles. The live archive has 400 articles, so that ceiling was approaching. [The article feed](../apps/web/app/components/ArticleFeed.tsx) initially renders 12 stories on the homepage or a category page and loads older stories through a browser `IntersectionObserver`, without a crawlable next-page URL. The sitemap exposes those older article URLs, but a normal HTML path through the archive would strengthen discovery and navigation.

## Follow-ups in priority order

1. Correct sitemap timestamps and cover the complete article archive before the 500-article ceiling is reached. Verify the resulting XML against the production API and representative pages. Preserve the existing 48-hour Google News sitemap window.
2. Add crawlable archive pagination with ordinary links to older articles, while retaining the current reader experience where useful. Check that pages are unique, canonical, and do not create duplicate feeds.
3. After deployment and another GSC read, inspect a small cohort of current AI articles from both unindexed groups. Record GSC's crawl date, referring sitemap or page, page fetch, declared and selected canonical, and whether the URL starts earning impressions. Use the sampled live test above as a baseline.
4. Review article quality and internal links for specific URLs that remain unindexed after Google crawls them. Some historical slugs in the discovered list appear outside the repository's current AI-only editorial policy; handle any editorial changes through `NEWS_PUBLISHING_POLICY.md`, not through a blanket SEO rewrite.

Do not start GSC's “Validate fix” workflow for the two Google-selected unindexed groups until a specific, testable site issue has been fixed. Their status alone does not identify such an issue.
