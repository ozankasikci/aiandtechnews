console.error(
  "This legacy bulk importer is disabled because it bypassed NEWS_PUBLISHING_POLICY.md. " +
  "Use scrape-news.ts for the daily RSS job or scrape-single.ts for one approved RSS item.",
);
process.exitCode = 1;
