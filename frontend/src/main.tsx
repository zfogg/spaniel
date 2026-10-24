import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ThemeProvider } from 'next-themes'
import { QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { MotionConfig } from 'motion/react'
import { queryClient } from './lib/query'
import './index.css'
import App from './App'
import { startBrowserTelemetry } from './lib/browser-telemetry'

const stopTelemetry =
  import.meta.env.VITE_SPANIEL_TELEMETRY === 'false' ? undefined : startBrowserTelemetry()
if (import.meta.hot && stopTelemetry) import.meta.hot.dispose(stopTelemetry)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <MotionConfig reducedMotion="user" transition={{ duration: 0.16, ease: [0.2, 0, 0, 1] }}>
        <ThemeProvider attribute="class" defaultTheme="system" disableTransitionOnChange>
          <App />
        </ThemeProvider>
      </MotionConfig>
      {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
    </QueryClientProvider>
  </StrictMode>,
)
