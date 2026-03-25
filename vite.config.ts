import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import path from 'path';
import {defineConfig} from 'vite';

export default defineConfig(() => {
  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, '.'),
      },
    },
    server: {
      hmr: true,
      host: '0.0.0.0',
      port: 5174,
      // Proxy API requests to Go backend
      proxy: {
        '/api': {
          target: 'http://localhost:3848',
          changeOrigin: true,
        },
      },
    },
  };
});
