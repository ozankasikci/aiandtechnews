import { Request, Response, NextFunction } from "express";
import jwt from "jsonwebtoken";

const JWT_EXPIRES_IN = "7d";

export interface JwtPayload {
  id: number;
  email: string;
  role: string;
}

declare global {
  namespace Express {
    interface Request {
      user?: JwtPayload;
    }
  }
}

export interface AuthService {
  generateToken(payload: JwtPayload): string;
  requireAuth(req: Request, res: Response, next: NextFunction): void;
}

export function createAuth(secret: string, now: () => number = Date.now): AuthService {
  if (!secret) throw new Error("JWT secret is required");
  return {
    generateToken(payload: JwtPayload): string {
      const iat = Math.floor(now() / 1_000);
      return jwt.sign({ ...payload, iat }, secret, { expiresIn: JWT_EXPIRES_IN });
    },
    requireAuth(req: Request, res: Response, next: NextFunction): void {
      const header = req.headers.authorization;
      if (!header || !header.startsWith("Bearer ")) {
        res.status(401).json({ error: "Authentication required" });
        return;
      }

      const token = header.slice(7);
      try {
        const decoded = jwt.verify(token, secret, {
          clockTimestamp: Math.floor(now() / 1_000),
        }) as JwtPayload;
        req.user = decoded;
        next();
      } catch {
        res.status(401).json({ error: "Invalid or expired token" });
      }
    },
  };
}
