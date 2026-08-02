import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        // SSE streams (balance / explorer) must not be buffered or idle-timed-out.
        timeout: 0,
        proxyTimeout: 0,
        configure: (proxy) => {
          // Some Node proxy paths drop Authorization — re-attach explicitly.
          proxy.on('proxyReq', (proxyReq, req) => {
            const auth = req.headers.authorization || req.headers.Authorization;
            if (auth) proxyReq.setHeader('Authorization', auth);
            const xTok = req.headers['x-auth-token'];
            if (xTok) proxyReq.setHeader('X-Auth-Token', xTok);
          });
          proxy.on('proxyRes', (proxyRes) => {
            if (String(proxyRes.headers['content-type'] || '').includes('text/event-stream')) {
              proxyRes.headers['cache-control'] = 'no-cache';
              proxyRes.headers['x-accel-buffering'] = 'no';
            }
          });
        },
      },
    },
  },
})
