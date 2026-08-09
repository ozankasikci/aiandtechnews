import { createHmac, timingSafeEqual } from "crypto";

export type NewsletterTokenPurpose = "confirm" | "unsubscribe";

interface TokenPayload {
  v: 1;
  id: number;
  purpose: NewsletterTokenPurpose;
  exp?: number;
}

function encode(value: string): string {
  return Buffer.from(value, "utf8").toString("base64url");
}

function sign(payload: string, secret: string): string {
  return createHmac("sha256", secret).update(payload).digest("base64url");
}

export function createNewsletterToken(
  subscriberId: number,
  purpose: NewsletterTokenPurpose,
  secret: string,
  expiresAt?: Date,
): string {
  const value: TokenPayload = {
    v: 1,
    id: subscriberId,
    purpose,
    ...(expiresAt ? { exp: Math.floor(expiresAt.getTime() / 1000) } : {}),
  };
  const payload = encode(JSON.stringify(value));
  return `${payload}.${sign(payload, secret)}`;
}

export function verifyNewsletterToken(
  token: string,
  expectedPurpose: NewsletterTokenPurpose,
  secret: string,
  now = new Date(),
): number | null {
  const [payload, suppliedSignature, extra] = token.split(".");
  if (!payload || !suppliedSignature || extra) return null;

  const expectedSignature = sign(payload, secret);
  const supplied = Buffer.from(suppliedSignature);
  const expected = Buffer.from(expectedSignature);
  if (supplied.length !== expected.length || !timingSafeEqual(supplied, expected)) return null;

  try {
    const value = JSON.parse(Buffer.from(payload, "base64url").toString("utf8")) as Partial<TokenPayload>;
    if (value.v !== 1 || value.purpose !== expectedPurpose || !Number.isInteger(value.id) || Number(value.id) < 1) {
      return null;
    }
    if (value.exp !== undefined && (!Number.isFinite(value.exp) || value.exp < Math.floor(now.getTime() / 1000))) {
      return null;
    }
    return Number(value.id);
  } catch {
    return null;
  }
}
