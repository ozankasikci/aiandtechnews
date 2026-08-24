import { NextRequest, NextResponse } from "next/server";

const API_BASE = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim() || "http://localhost:4001";

export async function GET(request: NextRequest) {
  const token = request.nextUrl.searchParams.get("token");
  if (!token) {
    return NextResponse.redirect(new URL("/newsletter/confirmed?state=invalid", request.url));
  }

  try {
    const response = await fetch(`${API_BASE}/api/newsletter/confirm?token=${encodeURIComponent(token)}`, {
      cache: "no-store",
    });
    const data = (await response.json()) as { state?: string };
    const state = response.ok && data.state ? data.state : "unavailable";
    return NextResponse.redirect(new URL(`/newsletter/confirmed?state=${encodeURIComponent(state)}`, request.url));
  } catch {
    return NextResponse.redirect(new URL("/newsletter/confirmed?state=unavailable", request.url));
  }
}
