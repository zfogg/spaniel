import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// The local Spaniel container is exposed on :8345. `make dev` supplies its
// own API URL for the backend it starts.
const apiTarget = process.env.SPANIEL_API_URL ?? 'http://127.0.0.1:8345'

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
