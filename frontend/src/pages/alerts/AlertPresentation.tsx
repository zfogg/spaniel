export const RuleSparkline = ({ values, color }: { values: number[]; color: string }) => {
  const width = 132,
    height = 34,
    padding = 3
  if (values.length < 2)
    return (
      <svg
        aria-label="Recent evaluation values are not available yet"
        className="h-8 w-full"
        preserveAspectRatio="none"
        role="img"
        viewBox={`0 0 ${width} ${height}`}
      >
        <path
          d={`M ${padding} ${height / 2} H ${width - padding}`}
          fill="none"
          opacity="0.45"
          stroke={color}
          strokeDasharray="3 3"
          strokeLinecap="round"
          strokeWidth="1.5"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
    )
  const min = Math.min(...values),
    max = Math.max(...values),
    span = max - min || 1
  const points = values
    .map(
      (value, index) =>
        `${(padding + (index / (values.length - 1)) * (width - padding * 2)).toFixed(1)},${(height - padding - ((value - min) / span) * (height - padding * 2)).toFixed(1)}`,
    )
    .join(' ')
  return (
    <svg
      aria-label={`Recent evaluation values from ${min.toLocaleString()} to ${max.toLocaleString()}`}
      className="h-8 w-full overflow-visible"
      preserveAspectRatio="none"
      role="img"
      viewBox={`0 0 ${width} ${height}`}
    >
      <polyline
        fill="none"
        points={points}
        stroke={color}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="2"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}
