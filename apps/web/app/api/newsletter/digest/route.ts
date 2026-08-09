import { NextRequest, NextResponse } from "next/server";

const API_BASE = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim() || "http://localhost:4001";

export const dynamic = "force-dynamic";
export const maxDuration = 300;

export async function GET(request: NextRequest) {
  const secret = process.env.CRON_SECRET?.trim();
  const authorization = request.headers.get("authorization");
  if (!secret || authorization !== `Bearer ${secret}`) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  try {
    const response = await fetch(`${API_BASE}/api/newsletter/digest`, {
      method: "POST",
      headers: { Authorization: authorization },
      cache: "no-store",
    });
    const data = await response.json();
    return NextResponse.json(data, { status: response.status });
  } catch {
    return NextResponse.json({ error: "Newsletter backend unavailable" }, { status: 502 });
  }
}
