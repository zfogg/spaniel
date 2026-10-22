const rowPitch = 44
const maxEntryDistance = 264

export const telemetryArrivalEase = [0.2, 0, 0, 1] as const

export type TelemetryArrivalMotion = {
  entryDistance: number
  duration: number
  velocity: number
}

/**
 * Keeps a burst compact: each later row travels farther from the header edge,
 * while duration grows much more slowly than that distance.
 */
export function telemetryArrivalMotion(
  arrivalIndex: number,
  arrivalCount: number,
): TelemetryArrivalMotion {
  const entryDistance = Math.min((arrivalIndex + 1) * rowPitch, maxEntryDistance)
  const duration = Math.min(0.46, 0.24 + Math.max(0, arrivalCount - 1) * 0.035)
  return { entryDistance, duration, velocity: entryDistance / duration }
}
