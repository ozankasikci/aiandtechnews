import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { ImageResponse } from "next/og";
import { getArticles } from "../../lib/api";
import { SHARE_CARD_SIZE, shareCardDateLabel, shareCardMoreHeadlines } from "../../lib/share-card";

// The article fetch revalidates every five minutes, and on demand when the API
// publishes (POST /api/revalidate), so the card follows the lead story.

export async function GET() {
  const [black, bold, data] = await Promise.all([
    readFile(join(process.cwd(), "assets/fonts/Inter-Black.ttf")),
    readFile(join(process.cwd(), "assets/fonts/Inter-Bold.ttf")),
    getArticles({ limit: 4 }),
  ]);
  const titles = (data?.articles ?? []).map((article) => article.title);
  const lead = titles[0] ?? "Breaking AI and technology news, daily.";
  const more = shareCardMoreHeadlines(titles);

  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: "60px 72px",
          background: "#1a1a1a",
          color: "#fff",
          fontFamily: "Inter",
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <div style={{ fontSize: 36, fontWeight: 900, letterSpacing: "-0.05em", textTransform: "uppercase" }}>
            AI &amp; Tech News
          </div>
          <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase", color: "#999" }}>
            {shareCardDateLabel(new Date())}
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column" }}>
          <div style={{ display: "flex", marginBottom: 20 }}>
            <div style={{ background: "#a855f7", color: "#000", fontSize: 20, fontWeight: 700, letterSpacing: "0.08em", padding: "5px 12px", borderRadius: 3 }}>
              LATEST
            </div>
          </div>
          <div style={{ fontSize: 62, fontWeight: 900, lineHeight: 1.08, letterSpacing: "-0.025em", maxHeight: 205, overflow: "hidden" }}>
            {lead}
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", borderTop: "2px solid #333", paddingTop: 22 }}>
          {more.map((title) => (
            <div key={title} style={{ fontSize: 24, fontWeight: 700, color: "#999", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", marginBottom: 6 }}>
              {title}
            </div>
          ))}
        </div>
      </div>
    ),
    {
      ...SHARE_CARD_SIZE,
      fonts: [
        { name: "Inter", data: black, weight: 900, style: "normal" },
        { name: "Inter", data: bold, weight: 700, style: "normal" },
      ],
    },
  );
}
