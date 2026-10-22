import { type ReactNode } from 'react'
import { LayoutGroup, motion, useReducedMotion } from 'motion/react'
import { telemetryArrivalEase, telemetryArrivalMotion } from '@/lib/telemetry-arrival-motion'
import { useNewItemIDs } from '@/lib/use-new-item-ids'

/** One telemetry row: existing rows reflow and new rows enter from the header edge. */
export function TelemetryArrival({
  arriving,
  arrivalIndex = 0,
  arrivalCount = 1,
  children,
}: {
  arriving: boolean
  arrivalIndex?: number
  arrivalCount?: number
  children: ReactNode
}) {
  const reduceMotion = useReducedMotion()
  const { entryDistance, duration } = telemetryArrivalMotion(arrivalIndex, arrivalCount)
  const transition = { duration, ease: telemetryArrivalEase }
  const initial = arriving && !reduceMotion ? { opacity: 0, y: -entryDistance } : false

  return (
    <motion.div
      data-motion-arrival={arriving ? 'true' : undefined}
      layout="position"
      initial={initial}
      animate={{
        opacity: 1,
        y: 0,
        backgroundColor:
          arriving && !reduceMotion ? ['var(--accent-bg)', 'transparent'] : 'transparent',
        boxShadow:
          arriving && !reduceMotion
            ? ['inset 3px 0 0 var(--accent)', 'inset 0 0 0 transparent']
            : 'inset 0 0 0 transparent',
      }}
      transition={{
        layout: transition,
        y: transition,
        opacity: { duration: Math.min(0.2, duration), ease: telemetryArrivalEase },
        backgroundColor: { duration: Math.max(0.38, duration * 1.6), ease: telemetryArrivalEase },
        boxShadow: { duration: Math.max(0.38, duration * 1.6), ease: telemetryArrivalEase },
      }}
    >
      {children}
    </motion.div>
  )
}

/** Shared list plumbing for traces, spans, and logs. */
export function TelemetryArrivalList<T>({
  items,
  trackedItems = items,
  itemKey,
  renderKey = itemKey,
  children,
}: {
  items: readonly T[]
  /** Full response used for arrival detection when this list is locally filtered. */
  trackedItems?: readonly T[]
  itemKey: (item: T) => string
  renderKey?: (item: T, index: number) => string
  children: (item: T, index: number) => ReactNode
}) {
  const arrivingIDs = useNewItemIDs(trackedItems, itemKey)
  const arrivalCount = items.reduce(
    (count, item) => count + Number(arrivingIDs.has(itemKey(item))),
    0,
  )
  let arrivalIndex = 0

  return (
    <LayoutGroup>
      {items.map((item, index) => {
        const arriving = arrivingIDs.has(itemKey(item))
        const currentArrivalIndex = arriving ? arrivalIndex++ : 0
        return (
          <TelemetryArrival
            key={renderKey(item, index)}
            arriving={arriving}
            arrivalIndex={currentArrivalIndex}
            arrivalCount={arrivalCount}
          >
            {children(item, index)}
          </TelemetryArrival>
        )
      })}
    </LayoutGroup>
  )
}
