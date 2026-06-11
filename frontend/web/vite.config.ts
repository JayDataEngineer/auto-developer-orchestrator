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
		dedupe: ["react", "react-dom", "zustand", "@assistant-ui/react", "@assistant-ui/react-markdown"],
	},
	server: {
		port: 5175,
		allowedHosts: ["pux.athleticnationalauthority.com", "orchestrator.local", "ubuntu-desktop.tailb1e597.ts.net"],
		proxy: {
			"/api": {
				target: "http://localhost:3847",
				changeOrigin: true,
				ws: true,
			},
		},
		watch: {
			ignored: ["**/next-app/**", "**/.next/**"],
		},
	},
	build: {
		outDir: resolve(__dirname, "../../dist-web"),
		emptyOutDir: true,
	},
});
