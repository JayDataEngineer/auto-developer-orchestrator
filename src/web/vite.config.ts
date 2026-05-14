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
			"@tui-core": resolve(__dirname, "../../ts-tui-pi/src/core"),
		},
	},
	server: {
		port: 5175,
		proxy: {
			"/api": {
				target: "http://localhost:3847",
				changeOrigin: true,
			},
		},
	},
	build: {
		outDir: resolve(__dirname, "../../dist-web"),
		emptyOutDir: true,
	},
});
