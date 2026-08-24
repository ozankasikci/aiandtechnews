import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("../src/index.ts", import.meta.url), "utf8");

// The dashboard router applies requireAuth to everything it is mounted on
// (routes/dashboard.ts). Both routers mount on "/api", so any /api route
// registered after the dashboard mount is swallowed by auth and answers 401.
test("the health route is registered before the auth-guarded routers", () => {
  const health = source.indexOf('app.get("/api/health"');
  const dashboardMount = source.indexOf('app.use("/api", dashboardRoutes)');

  assert.ok(health !== -1, "expected a /api/health route");
  assert.ok(dashboardMount !== -1, "expected the dashboard router to mount on /api");
  assert.ok(
    health < dashboardMount,
    "/api/health must be registered before the dashboard router or requireAuth returns 401",
  );
});
