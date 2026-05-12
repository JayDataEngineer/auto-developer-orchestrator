import { defineConfig } from "vite";
import { resolve } from "path";

export default defineConfig({
	root: __dirname,
	resolve: {
		alias: {
			"@root": resolve(__dirname, "../.."),
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
