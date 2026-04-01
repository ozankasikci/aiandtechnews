import { NextRequest, NextResponse } from "next/server";

const API_BASE = process.env.API_URL || "http://localhost:4001";

export async function GET(request: NextRequest) {
  const q = request.nextUrl.searchParams.get("q") || "";
  const limit = request.nextUrl.searchParams.get("limit") || "20";

  try {
    const url = `${API_BASE}/api/articles?search=${encodeURIComponent(q)}&limit=${limit}`;
    const res = await fetch(url, { next: { revalidate: 0 } });
    const data = await res.json();
    return NextResponse.json(data);
  } catch {
    return NextResponse.json({ articles: [], total: 0 }, { status: 500 });
  }
}
