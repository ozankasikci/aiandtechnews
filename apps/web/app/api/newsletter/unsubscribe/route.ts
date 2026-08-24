import { NextRequest, NextResponse } from "next/server";

const API_BASE = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim() || "http://localhost:4001";

async function forwardUnsubscribe(request: NextRequest) {
  const token = request.nextUrl.searchParams.get("token");
  if (!token) return { responseOk: false, state: "invalid" };

  try {
    const response = await fetch(`${API_BASE}/api/newsletter/unsubscribe?token=${encodeURIComponent(token)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token }),
      cache: "no-store",
    });
    const data = (await response.json()) as { state?: string };
    return { responseOk: response.ok, state: response.ok && data.state ? data.state : "unavailable" };
  } catch {
    return { responseOk: false, state: "unavailable" };
  }
}

export async function GET(request: NextRequest) {
  const result = await forwardUnsubscribe(request);
  return NextResponse.redirect(new URL(`/newsletter/unsubscribed?state=${encodeURIComponent(result.state)}`, request.url));
}

export async function POST(request: NextRequest) {
  const result = await forwardUnsubscribe(request);
  return NextResponse.json({ state: result.state }, { status: result.responseOk ? 200 : 503 });
}
