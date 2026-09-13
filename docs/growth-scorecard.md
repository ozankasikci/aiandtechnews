# Weekly growth scorecard

Review every Monday using the latest complete seven-day window available in each product. Record the exact dates shown in Google Analytics and Search Console because their reporting delays differ. Do not add Search Console clicks to Analytics sessions; they measure different things.

## Baseline captured 13 September 2026

| Signal | Value | Reporting window | Source |
| --- | ---: | --- | --- |
| Active users | 57 | 6-12 September | [GA4 Home](https://analytics.google.com/analytics/web/#/a50966727p472081893/reports/intelligenthome) |
| New users | 55 | 6-12 September | GA4 Home |
| Organic Search sessions | 5 | Last 7 days shown on 13 September | GA4 Home, sessions by channel |
| Signup key events | 0 | 6-12 September | GA4 Home. Tracking fix was not yet deployed. |
| Google Search clicks | 5 | 5-11 September | [Search Console Performance](https://search.google.com/search-console/performance/search-analytics?resource_id=sc-domain%3Aaiandtech.news&num_of_days=7) |
| Google Search impressions | 892 | 5-11 September | Search Console Performance |
| Google Search CTR | 0.6% | 5-11 September | Search Console Performance |
| Indexed pages | 126 | Report last updated 4 September | [Search Console Page indexing](https://search.google.com/search-console/index?resource_id=sc-domain%3Aaiandtech.news) |
| Not indexed pages | 82 | Report last updated 4 September | Search Console Page indexing: 73 discovered, 7 crawled, 1 redirect, 1 historical 404 |

Both [submitted sitemaps](https://search.google.com/search-console/sitemaps?resource_id=sc-domain%3Aaiandtech.news) showed Success and were last read on 12 September. The historical 404 URL returned HTTP 200 on 13 September, and Search Console validation was started that day. These are snapshots, not proof that all pages are indexed.

## Weekly update template

| Week and source window | Search clicks | Search impressions | Search CTR | Organic Search sessions | Active users | Engaged sessions | New users | Returning users | `generate_lead` key events | Signups by channel | Notes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- |
| Date range | | | | | | | | | | | |

Check these each week:

1. Confirm the production smoke check passes and the GA4 web stream `G-32SP4ZKM67` is receiving data.
2. Record Search Console Performance for the latest complete seven days, plus indexing reasons and sitemap status. Investigate sudden changes before requesting indexing or altering crawl rules.
3. Record GA4 acquisition, engagement, new and returning users, and `generate_lead` key events. Segment key events by session channel and `placement` where available. The event represents a new or reactivated newsletter subscription, not an already-active address.
4. Compare with the prior equivalent window. Note outages, reporting lag, small sample sizes, and content or campaign changes.
5. Select one action for the following week based on engaged readers and signups, not impressions alone.

## Four-week developer-reader experiment

Target one audience: people building with AI tools. Twice a week, choose an already published, genuinely useful story or explainer and write a short platform-specific summary that highlights one verified takeaway. Before selecting or promoting any article, confirm it complies with `NEWS_PUBLISHING_POLICY.md` and that its public page works. Prepare posts for relevant developer communities, but follow each community's rules and obtain approval before publishing. Do not mass-post the same copy.

Use a distinct tracked link for each placement, such as `?utm_source=reddit&utm_medium=social&utm_campaign=developer_readers_sep2026`. Record publication date, destination, article URL, visits, engaged sessions, and signups. After four weeks, keep the placements that produce engaged readers or subscribers and stop the ones that only produce impressions. No posts have been published as part of this scorecard.
