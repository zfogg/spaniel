import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// The local development daemon serves the API on :8080. `make dev` can still
// override this when it starts a dedicated backend.
const apiTarget = process.env.SPANIEL_API_URL ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [tailwindcss(), react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    // Node by default; tsx component tests opt into jsdom via the file-level
    // // @vitest-environment jsdom pragma.
    environment: 'node',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
  server: {
    port: 5173,
    proxy: {
      '/api': apiTarget,
      '/mcp': apiTarget,
      '/ws': {
        target: apiTarget.replace(/^http/, 'ws'),
        ws: true,
      },
    },
  },
})
