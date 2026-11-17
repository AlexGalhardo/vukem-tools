import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "astro/config";

const siteUrl = process.env.SITE_URL || "https://tools.vukem.dev";

export default defineConfig({
	site: siteUrl,
	output: "static",
	trailingSlash: "always",
	build: { format: "directory" },
	vite: {
		plugins: [tailwindcss()],
	},
});
