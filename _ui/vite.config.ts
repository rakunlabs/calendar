import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  base: './',
  plugins: [svelte(), tailwindcss()],
  server: {
    proxy: {
      '/v1': {
        target: process.env.CALENDAR_API_URL || 'http://localhost:8080',
        rewrite: (path) => `/calendar${path}`,
      },
      '/calendar/v1': process.env.CALENDAR_API_URL || 'http://localhost:8080',
    },
  },
});
