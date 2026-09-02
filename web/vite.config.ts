import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  resolve: {
    alias: {
      $lib: fileURLToPath(new URL('./src/lib', import.meta.url)),
    },
  },
  server: {
    host: 'localhost',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:' + (process.env.STREAMLINE_API_PORT ?? '8080'),
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../internal/webassets/dist',
    emptyOutDir: true,
    sourcemap: true,
  },
});

