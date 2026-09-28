# Four-week organic-search experiment

**Run window:** 28 September to 25 October 2026. Review the first complete 28-day outcome window after the run as well, because discovery and indexing can lag publication. This is an experiment plan, not authorization to publish articles, change the feed allowlist, increase the publication limit, or enable the Go publisher.

## Starting point and goal

The [Search Console Search results report](https://search.google.com/search-console/performance/search-analytics?resource_id=sc-domain%3Aaiandtech.news&num_of_days=28) showed **81 clicks, 5.16k impressions, 1.6% CTR, and average position 20.9** for 29 August to 25 September. The `meta ai` query had 22 clicks from 152 impressions. One Nitter article had 778 impressions but 2 clicks. The [Page indexing report](https://search.google.com/search-console/index?resource_id=sc-domain%3Aaiandtech.news), last updated 21 September, showed 230 indexed pages, 74 discovered but not indexed, and 10 crawled but not indexed. These are snapshots with different reporting dates, not a count of current users or proof of a particular indexing cause.

The goal is more **engaged readers and newsletter signups from organic search**, not more URLs or impressions alone. Use the existing [weekly growth scorecard](growth-scorecard.md) for site-wide trends. Do not add Search Console clicks to GA4 sessions: [Google explains that they measure different events](https://developers.google.com/search/docs/monitor-debug/google-analytics-search-console).

## Three testable hypotheses

1. **Discovery:** A complete ordinary sitemap with truthful modification dates, a current two-day News sitemap, and working canonical article pages will make eligible articles easier to discover. This may improve indexing coverage, but a sitemap does not guarantee indexing. [Google's News sitemap guidance](https://developers.google.com/search/docs/crawling-indexing/sitemaps/news-sitemap) limits news entries to the last two days.
2. **Search-result fit:** For an already indexed article that earns impressions at a reasonably visible position, a clearer, fully factual title and description may earn more clicks than its current presentation. Investigate the Nitter page first, including its actual queries, position, Google-selected title, and snippet. Its 2/778 CTR alone does not prove a title problem.
3. **Reader value:** Within the existing publication cadence, a small number of AI news items that answer concrete reader questions and add verified context may bring more engaged organic readers than generic rewrites. Google's [people-first guidance](https://developers.google.com/search/docs/fundamentals/creating-helpful-content) favors original value and warns against creating pages merely to capture queries. This hypothesis does not justify publishing more articles or bypassing editorial checks.

## Selection and editorial boundaries

Read `NEWS_PUBLISHING_POLICY.md` before selecting or editing any article. A trial candidate must originate from an approved **current RSS feed**, not merely an approved domain, and must be explicitly AI-related news. Reject reviews, buying guides, deals and other promotional items, old reposts, PDFs, videos as items, and unclear AI relevance. Check the canonical `source_url` and proposed slug for duplicates, then repeat immediately before insertion. The original source URL remains in `source_url`, with no source footer in the body. All writing, metadata, and revisions must be accurate, original, non-clickbait, and free of em dashes.

**Cadence hypothesis, not a policy change:** Review up to two search-relevant candidate stories per week for possible inclusion in the existing legacy importer cadence of at most one article per run and four slots per day. Publish no extra slots. Do not fill a slot with a weak or non-compliant candidate. If the active workflow cannot safely make an editorial selection, leave the publisher untouched and measure normally published compliant stories instead. Never run the legacy and Go publishers concurrently.

Select candidates for reader value first: a specific verified development, a clearly affected audience, and source-supported context or next step. Search Console queries can reveal how readers describe the topic, but must not determine unsupported claims or force a story outside the policy. No article drafts or publication are part of this document.

## Weekly sequence

| Week | Work | Evidence to record |
| --- | --- | --- |
| 1, 28 Sep to 4 Oct | Ship and smoke-check sitemap fixes. Reconcile ordinary sitemap article URLs against the published API total. Check a sample of article pages for 200 status, self-canonical, server-rendered headline/body, and valid dates. Investigate representative URLs from each non-indexed bucket before requesting any revalidation. | Sitemap counts and status; 10 sampled URL inspection results across indexed, discovered, and crawled buckets; exact deployment date. |
| 2, 5 to 11 Oct | Choose up to two already indexed, high-impression pages for a title/description review. Only change a page when query intent, its actual ranking position, and source facts support a clearer presentation. Keep a comparable, unedited group of pages. Record any compliant new-story candidates handled within current slots. | Old and new metadata, reason for each change, query/page/device data, matched control URLs, publication and change timestamps. |
| 3, 12 to 18 Oct | Repeat the bounded editorial review. Inspect newly published article URLs for discovery and indexing status at 7 and 14 days of age. Check whether treatment pages also produce engaged readers, not just search appearances. | Per-URL Search Console clicks, impressions, CTR, position, indexing status; GA4 organic landing sessions, engaged sessions, and `generate_lead`. |
| 4, 19 to 25 Oct | Stop making test changes and review the first equal-length outcome windows. Investigate failures before expanding anything. Schedule a 28-day cohort follow-up for articles published late in the run. | Site-wide scorecard, treated versus comparison pages, editorial workload, and a keep/change/stop decision. |

## Measurement and decision rules

Use **Search Console Web Search** for discovery and search-result measures, with the same country/device/search-type filters and complete dates on both sides of a comparison. Segment by page and query. Compare titles only among pages with similar rank and topic; a position shift, changing news demand, or a Google-rewritten title can explain CTR movement. [Google recommends investigating page and query patterns rather than relying on absolute average position](https://developers.google.com/search/docs/monitor-debug/debugging-search-traffic-drops). Record News and Discover separately if available, not inside the Web Search test.

Use **GA4 Organic Search landing sessions**, engaged sessions, and `generate_lead` for post-click quality and newsletter conversion. Record the actual source window because GA4 and Search Console can lag differently. A signup rate with fewer than 10 organic signups is descriptive, not a robust comparison.

Track each test URL with: cohort (`metadata treatment`, `unchanged comparison`, or `new article`); publication and edit dates; policy-compliant source feed; query cluster; Google-selected canonical; indexing state and last crawl; and Search Console impressions, clicks, CTR, and position at 7, 14, and 28 days after publication or edit. Add GA4 organic landing sessions, engaged sessions, and `generate_lead` for the same date windows. Use equal-age windows for new articles. Do not compare a two-day-old story with an article that has had a full month to rank.

At the end of week 4:

- **Keep** the sitemap repair if URL coverage is complete, sample canonical pages are healthy, and no false modification dates appear. If indexing does not improve, diagnose the indexed and excluded URL samples instead of increasing publication volume.
- **Consider extending** the metadata treatment only if treated pages have more clicks per comparable impressions than their controls, ranking and query mix are broadly stable, and GA4 engagement does not decline. Report the numerator and denominator, not just a percentage. With a small sample or fewer than 20 clicks across the compared pages, label the result *directional*, not proven.
- **Keep or stop** the editorial cadence hypothesis based on 7- and 28-day organic engaged sessions and signups per article, together with editor effort and policy pass rate. If there is no measurable lift or too few indexed pages, refine quality and discovery before scaling.

The 81-click baseline is too small to promise a statistically decisive four-week result. A successful sprint can still produce a trustworthy baseline, fixed coverage, and a clear next decision without claiming that sitemap submission or extra publishing guarantees rankings.
