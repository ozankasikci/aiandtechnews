// published_at round trip for the article editor's <input type="datetime-local">.
//
// The picker shows and returns wall-clock time in the browser's zone with no
// offset. The API stores published_at as sent and reads offset-less values in
// the server's zone, so the editor converts both ways: stored values are shown
// in local time, and picked values are sent as UTC ISO strings ("...Z"),
// which the Node and Go servers parse identically.

const SQLITE_TIMESTAMP = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/;

/** Reads a stored published_at like the API does: SQLite's "YYYY-MM-DD HH:MM:SS" is UTC. */
export function parseStoredDate(value: string | null | undefined): Date | null {
  if (!value) return null;
  const parsed = new Date(SQLITE_TIMESTAMP.test(value) ? `${value.replace(" ", "T")}Z` : value);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

/** Formats an instant as a datetime-local value in the browser's time zone. */
export function toDateTimeLocalValue(date: Date | null): string {
  if (!date) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** Converts a datetime-local value (browser local time) to a UTC ISO string. */
export function fromDateTimeLocalValue(value: string): string | null {
  if (!value) return null;
  // A date-time string without an offset is local time (ECMAScript Date).
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? null : parsed.toISOString();
}
