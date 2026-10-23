import { useEffect, useRef, useState } from 'react'

// Measure rendered SVG labels, including the actual font, instead of reserving a fixed margin.
export function useAxisGutter(labels: string[], fontSize: number) {
  const axisRef = useRef<SVGGElement>(null)
  const key = labels.join('\n')
  const estimate = Math.max(0, ...labels.map((label) => label.length * fontSize * 0.62)) + 12
  const [measured, setMeasured] = useState<{ key: string; width: number }>()
  useEffect(() => {
    let active = true
    const measure = () => {
      const texts = axisRef.current?.querySelectorAll('text')
      if (!active || !texts?.length) return
      const widths = Array.from(texts).map((text) =>
        typeof text.getComputedTextLength === 'function' ? text.getComputedTextLength() : 0,
      )
      const width = Math.max(...widths)
      if (width > 0) setMeasured({ key, width: Math.ceil(width) + 12 })
    }
    measure()
    void document.fonts?.ready.then(measure)
    return () => {
      active = false
    }
  }, [key])
  return { axisRef, gutter: measured?.key === key ? measured.width : estimate }
}
