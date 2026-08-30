import { NextRequest, NextResponse } from "next/server";
import { getArticlesUpTo } from "../../lib/api";
import {
  isAuthorizedCronRequest,
  recentlyChangedArticleUrls,
  submitIndexNowUrls,
} from "../../lib/indexnow";

export const dynamic = "force-dynamic";
export const maxDuration = 60;

export async function GET(request: NextRequest) {
  if (!isAuthorizedCronRequest(request.headers.get("authorization"), process.env.CRON_SECRET)) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const articles = await getArticlesUpTo(500);
  if (!articles) {
    return NextResponse.json({ error: "Article backend unavailable" }, { status: 502 });
  }

  const urls = recentlyChangedArticleUrls(articles);

  try {
    const result = await submitIndexNowUrls(urls);
    return NextResponse.json({ success: true, ...result });
  } catch (error) {
    console.error("IndexNow catch-up failed", error);
    return NextResponse.json({ error: "IndexNow submission failed" }, { status: 502 });
  }
}
