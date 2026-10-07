import type { MetricSeriesPoint } from '@/lib/api'

export type MetricType = 'gauge' | 'counter' | 'histogram'

export interface BucketedSeries {
  /** Single series for gauge/counter; for histograms this is the p95 array (used as the sparkline source). */
  values: number[]
  p50?: number[]
  p95?: number[]
  p99?: number[]
}

/**
 * bucketPoints buckets raw OTLP data points into `bins` evenly-spaced bins
 * along the time axis [tMin, tMax]. The last value in each bin wins. Empty
 * bins are forward-filled from the previous filled bin so the chart renders
 * continuous lines instead of dropping to zero.
 *
 * Histogram percentiles are calculated from the original bucket counts at
 * render time, never from ingest-time synthetic rows.
 */
export function bucketPoints(
  points: MetricSeriesPoint[],
  type: MetricType,
  bins = 60,
): BucketedSeries {
  if (points.length === 0) {
    return { values: new Array(bins).fill(0) }
  }
  const tMin = points[0].timestamp_ns
  const tMax = points[points.length - 1].timestamp_ns
  const span = Math.max(1, tMax - tMin)

  if (type === 'histogram') {
    const out: Record<'p50' | 'p95' | 'p99', number[]> = {
      p50: new Array(bins).fill(NaN),
      p95: new Array(bins).fill(NaN),
      p99: new Array(bins).fill(NaN),
    }
    // Histogram API requests contain the server-derived scalar for the
    // selected operation in `value` (avg, p50, p90, …). Preserve that series
    // for the main chart; the per-bucket percentiles below remain available
    // for the summary strip and raw bucket inspection.
    const values = new Array(bins).fill(NaN)
    for (const p of points) {
      const i = Math.min(bins - 1, Math.floor(((p.timestamp_ns - tMin) / span) * bins))
      // A derived histogram value of zero after a prior cumulative snapshot
      // means no new observations arrived in that export interval. Leave the
      // bin empty so forwardFill keeps the last observed percentile instead
      // of drawing a misleading fall to a literal zero-sized observation.
      if (p.value !== 0) values[i] = p.value
      if (p.bounds?.length && p.buckets?.length) {
        out.p50[i] = histogramPercentile(p.bounds, p.buckets, 0.5)
        out.p95[i] = histogramPercentile(p.bounds, p.buckets, 0.95)
        out.p99[i] = histogramPercentile(p.bounds, p.buckets, 0.99)
      } else if (p.percentile) {
        out[p.percentile][i] = p.value
      } else {
        out.p50[i] = p.value
        out.p95[i] = p.value
        out.p99[i] = p.value
      }
    }
    for (const k of ['p50', 'p95', 'p99'] as const) {
      forwardFill(out[k])
    }
    forwardFill(values)
    return { values, p50: out.p50, p95: out.p95, p99: out.p99 }
  }

  const values = new Array(bins).fill(NaN)
  for (const p of points) {
    const i = Math.min(bins - 1, Math.floor(((p.timestamp_ns - tMin) / span) * bins))
    values[i] = p.value
  }
  forwardFill(values)
  return { values }
}

function histogramPercentile(bounds: number[], counts: number[], q: number) {
  const total = counts.reduce((sum, n) => sum + n, 0)
  if (!total) return 0
  const target = total * q
  let cumulative = 0
  for (let i = 0; i < counts.length; i++) {
    const next = cumulative + counts[i]
    if (next >= target) {
      if (i >= bounds.length) return bounds.length ? bounds[bounds.length - 1] : 0
      const lo = i ? bounds[i - 1] : 0
      return counts[i] ? lo + ((target - cumulative) / counts[i]) * (bounds[i] - lo) : bounds[i]
    }
    cumulative = next
  }
  return bounds.length ? bounds[bounds.length - 1] : 0
}

function forwardFill(arr: number[]) {
  let last = NaN
  for (let i = 0; i < arr.length; i++) {
    if (isNaN(arr[i])) {
      arr[i] = isNaN(last) ? 0 : last
    } else {
      last = arr[i]
    }
  }
}
