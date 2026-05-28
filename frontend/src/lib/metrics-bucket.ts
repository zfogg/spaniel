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
 * For histograms the function returns separate p50/p95/p99 arrays, each
 * derived from the points whose `percentile` attribute matches.
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
    for (const p of points) {
      if (!p.percentile) continue
      const i = Math.min(bins - 1, Math.floor(((p.timestamp_ns - tMin) / span) * bins))
      out[p.percentile][i] = p.value
    }
    for (const k of ['p50', 'p95', 'p99'] as const) {
      forwardFill(out[k])
    }
    return { values: out.p95, p50: out.p50, p95: out.p95, p99: out.p99 }
  }

  const values = new Array(bins).fill(NaN)
  for (const p of points) {
    const i = Math.min(bins - 1, Math.floor(((p.timestamp_ns - tMin) / span) * bins))
    values[i] = p.value
  }
  forwardFill(values)
  return { values }
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
