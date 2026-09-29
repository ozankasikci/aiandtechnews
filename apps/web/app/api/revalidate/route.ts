import { revalidatePath, revalidateTag } from "next/cache";
import { handleRevalidateRequest } from "../../lib/revalidate";

export const dynamic = "force-dynamic";

// Called by the API (SITE_REVALIDATE_URL) with Authorization: Bearer
// CRON_SECRET and {"slugs": [...]} after an article changes.
export async function POST(request: Request) {
  return handleRevalidateRequest(request, {
    secret: process.env.CRON_SECRET,
    revalidateTag,
    revalidatePath,
  });
}
