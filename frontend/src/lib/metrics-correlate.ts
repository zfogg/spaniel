import type { MetricSeriesPoint, TraceOverlay } from '@/lib/api'

// xPosForTrace maps a trace's `start_ns` to a bin index along the chart's
// x-axis. The chart renders `bins` discrete buckets — bin 0 lives at
// points[0].timestamp_ns, bin (bins-1) at points[last].timestamp_ns,
// linearly interpolated between.
//
// Edge cases:
//   - Trace before the first point  →  0 (clamped left)
//   - Trace after the last point    →  bins-1 (clamped right)
//   - Empty / one point of metrics  →  null (no horizontal axis to map onto)
export function xPosForTrace(
  trace: Pick<TraceOverlay, 'start_ns'>,
  points: Pick<MetricSeriesPoint, 'timestamp_ns'>[],
  bins: number,
): number | null {
  if (bins <= 0 || points.length < 2) return null
  const tMin = points[0].timestamp_ns
  const tMax = points[points.length - 1].timestamp_ns
  const span = tMax - tMin
  if (span <= 0) return null
  const ratio = (trace.start_ns - tMin) / span
  if (ratio <= 0) return 0
  if (ratio >= 1) return bins - 1
  return Math.round(ratio * (bins - 1))
}

// valueAtBin returns the metric value at a given bin index, used by the
// correlated-traces table to show "the value at the time this trace ran".
// Falls back to the nearest neighbor if the bin is out of range or sparse.
export function valueAtBin(values: number[], bin: number | null): number | null {
  if (bin == null || values.length === 0) return null
  if (bin < 0) return values[0]
  if (bin >= values.length) return values[values.length - 1]
  return values[bin]
}
