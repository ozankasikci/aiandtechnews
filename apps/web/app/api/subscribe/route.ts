import { NextRequest, NextResponse } from "next/server";

const API_BASE = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim() || "http://localhost:4001";

// The API limits signups per client IP. This route runs on Vercel, so pass the
// visitor's address along; otherwise every website signup would share Vercel's.
function visitorIp(request: NextRequest): string | null {
  const forwarded = request.headers.get("x-forwarded-for")?.split(",")[0]?.trim();
  return forwarded || request.headers.get("x-real-ip")?.trim() || null;
}

export async function POST(request: NextRequest) {
  const body = await request.json();
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const ip = visitorIp(request);
  if (ip) headers["X-Forwarded-For"] = ip;
  try {
    const res = await fetch(`${API_BASE}/api/subscribe`, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    });
    const data = await res.json();
    return NextResponse.json(data, { status: res.status });
  } catch {
    return NextResponse.json({ error: "Failed to subscribe" }, { status: 500 });
  }
}
