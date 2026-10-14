// Helpers for time series from the metrics API.

/**
 * Metrics are only recorded while the panel runs. Missing buckets mean nothing
 * was running, so they are filled with zero points instead of drawing a
 * straight line across the gap.
 */
export function fillGaps<T extends { t: number }>(points: T[], bucket: number, zero: (t: number) => T, until = Date.now() / 1000): T[] {
  if (!bucket || points.length === 0) return points;
  const byBucket = new Map(points.map((p) => [Math.floor(p.t / bucket) * bucket, p]));
  const start = Math.floor(points[0].t / bucket) * bucket;
  const end = Math.floor(until / bucket) * bucket;
  const out: T[] = [];
  for (let t = start; t <= end && out.length < 5000; t += bucket) out.push(byBucket.get(t) ?? zero(t));
  return out;
}
