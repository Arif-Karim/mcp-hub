/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const daemon = 'http://127.0.0.1:7420'

// `vite --mode mock` (npm run dev:mock) swaps the HTTP client for the in-memory mock.
export default defineConfig(({ mode }) => ({
  plugins: [vue()],
  define: mode === 'mock' ? { 'import.meta.env.VITE_API_MODE': JSON.stringify('mock') } : {},
  server: {
    proxy: {
      '/api': daemon,
      '/mcp': daemon,
      '/.well-known': daemon,
    },
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
  },
}))
