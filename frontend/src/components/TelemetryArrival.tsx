import { useEffect, useState } from 'react'
import { motion } from 'motion/react'

const arrivalDurationMs = 5_000
const arrivalTransition = { duration: arrivalDurationMs / 1_000, ease: [0.2, 0, 0, 1] as const }
const resting = { opacity: 1, backgroundColor: 'transparent', boxShadow: 'inset 0 0 0 transparent' }

/** A subtle first-seen cue for live telemetry rows. */
export function TelemetryArrival({
  arriving,
  children,
}: {
  arriving: boolean
  children: React.ReactNode
}) {
  // Starting this after mount matters: `initial` on a newly inserted row can
  // be skipped when its parent updates within the same React commit.
  const [isArriving, setIsArriving] = useState(false)

  useEffect(() => {
    if (!arriving) return
    setIsArriving(true)
    const timer = window.setTimeout(() => setIsArriving(false), arrivalDurationMs)
    return () => window.clearTimeout(timer)
  }, [arriving])

  return (
    <motion.div
      data-motion-arrival={isArriving ? 'true' : undefined}
      initial={false}
      animate={
        isArriving
          ? {
              opacity: [0.35, 1],
              backgroundColor: ['var(--accent-bg)', 'transparent'],
              boxShadow: ['inset 4px 0 0 var(--accent)', 'inset 0 0 0 transparent'],
            }
          : resting
      }
      transition={arrivalTransition}
    >
      {children}
    </motion.div>
  )
}
