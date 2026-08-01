import { NextRequest, NextResponse } from "next/server";

const API_BASE = (process.env.API_URL || process.env.NEXT_PUBLIC_API_URL)?.trim() || "http://localhost:4001";

export async function POST(request: NextRequest) {
  const body = await request.json();
  try {
    const res = await fetch(`${API_BASE}/api/subscribe`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    return NextResponse.json(data, { status: res.status });
  } catch {
    return NextResponse.json({ error: "Failed to subscribe" }, { status: 500 });
  }
}
