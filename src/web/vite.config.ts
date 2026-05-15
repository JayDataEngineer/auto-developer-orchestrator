import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
	plugins: [react(), tailwindcss()],
	root: __dirname,
	resolve: {
		alias: {
			"@": resolve(__dirname, "src"),
			"@pux/shared": resolve(__dirname, "../../shared/src/index.ts"),
		},
	},
	server: {
		port: 5175,
		proxy: {
			"/api": {
				target: "http://localhost:3847",
				changeOrigin: true,
				ws: true,
			},
		},
	},
	build: {
		outDir: resolve(__dirname, "../../dist-web"),
		emptyOutDir: true,
	},
});
