import { motion } from 'motion/react'

const arrivalTransition = { duration: 0.16, ease: [0.2, 0, 0, 1] as const }

/** A subtle first-seen cue for live telemetry rows. */
export function TelemetryArrival({
  arriving,
  children,
}: {
  arriving: boolean
  children: React.ReactNode
}) {
  return (
    <motion.div
      initial={arriving ? { opacity: 0.45, boxShadow: 'inset 2px 0 0 var(--accent)' } : false}
      animate={{ opacity: 1, boxShadow: 'inset 0 0 0 transparent' }}
      transition={arrivalTransition}
    >
      {children}
    </motion.div>
  )
}
