import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  root: here,
  resolve: {
    alias: { "@": resolve(here, "src") },
    dedupe: ["react", "react-dom"],
  },
  server: {
    port: 5176,
    strictPort: true,
    proxy: {
      "/api": { target: "http://127.0.0.1:3001", changeOrigin: true, ws: true },
    },
  },
  build: { outDir: resolve(here, "dist"), emptyOutDir: true },
});
