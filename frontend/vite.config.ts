import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // Match the layout the Docker/nginx layer already expects: the final image
    // copies frontend/build -> nginx docroot, and nginx cache-optimizes /static/.
    // Keeping outDir 'build' + assetsDir 'static' means the Dockerfiles and all
    // nginx configs need no changes.
    outDir: 'build',
    assetsDir: 'static',
    sourcemap: false,
  },
  server: {
    port: 3000,
    // The app talks to the backend over relative URLs; in the container nginx
    // proxies /api. For a standalone `npm run dev`, proxy /api (REST + the
    // notifications WebSocket) to the backend so dev works without nginx.
    proxy: {
      '/api': {
        target: 'https://localhost:31337',
        changeOrigin: true,
        secure: false,
        ws: true,
      },
    },
  },
});
