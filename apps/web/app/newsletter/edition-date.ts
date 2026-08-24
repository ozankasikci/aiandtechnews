// Edition keys are calendar dates (YYYY-MM-DD) in the publication timezone,
// so they are formatted as plain dates rather than parsed as instants.
export function formatEditionDate(editionKey: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(editionKey.trim());
  if (!match) return editionKey;

  const [, year, month, day] = match;
  const date = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day)));

  // Date.UTC rolls impossible values over (month 13 becomes January of the
  // next year), so an out-of-range key is only caught by comparing back.
  const roundTrips =
    date.getUTCFullYear() === Number(year) &&
    date.getUTCMonth() === Number(month) - 1 &&
    date.getUTCDate() === Number(day);
  if (!roundTrips) return editionKey;

  return date.toLocaleDateString("en-US", {
    weekday: "long",
    month: "long",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}
