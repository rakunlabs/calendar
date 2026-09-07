import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  base: '/calendar/',
  plugins: [svelte(), tailwindcss()],
  server: { proxy: { '/calendar/v1': process.env.CALENDAR_API_URL || 'http://localhost:8080' } },
});
