/** Copy of the landing page (English only). */

export type Feature = {
	id: string;
	tab: string;
	title: string;
	text: string;
	command: string;
	image?: { src: string; alt: string };
};

export const landing = {
	title: "Vukem Tools: one catalogue for the browser, the desktop window, and agents",
	description:
		"An open source file conversion toolbox. The same tools run in the browser, in a desktop window, and from the CLI, HTTP API, and MCP.",
	hero: {
		title: "Convert a file. The same tool, everywhere.",
		lead: "Vukem Tools is an open source conversion toolbox. A person uses the web app or the desktop window. An agent uses the same catalogue from the command line, HTTP, or MCP.",
		runLabel: "Run the stack with Docker Compose. Set a key, then start it:",
		runNote: "Needs Docker. The app listens on http://127.0.0.1:3000. Production also needs the variables in the deployment document.",
		points: [
			"One catalogue for the browser, the window, and agents.",
			"PDF, image, audio, video, archives, and sprites.",
			"Login required. API keys carry abilities.",
			"Free and open source (MIT).",
		],
		quickStart: "Crop a PDF",
		releases: "Releases",
	},
	gallery: {
		label: "The toolbox",
	},
	features: [
		{
			id: "crop",
			tab: "PDF crop",
			title: "Crop every page",
			text: "Trim the same margin from every page of a PDF. The web form, the CLI, the HTTP API, and MCP call the same crop.",
			command: "vukem-tools pdf crop --input in.pdf --output out.pdf",
			image: {
				src: "/screens/pdf-crop.png",
				alt: "The Crop a PDF page: a drop zone for the file, the options panel and the Crop a PDF button.",
			},
		},
		{
			id: "catalogue",
			tab: "Catalogue",
			title: "One list of tools",
			text: "Image, audio, video, PDF, archives, and sprite sheets are entries in one catalogue. A browser-only tool is marked and does not need the server pipeline.",
			command: "vukem-tools tools",
			image: {
				src: "/screens/catalogue.png",
				alt: "The tool catalogue: the families in the sidebar, a search field and the list of tools with their formats.",
			},
		},
		{
			id: "agents",
			tab: "Agents",
			title: "Agents use the same names",
			text: "An agent lists the tools, then runs any of them through the CLI, a local HTTP server, or MCP, with an API key that carries abilities. The tool names are the same on every surface, and the HTTP API has a generated reference.",
			command: "vukem-tools mcp",
			image: {
				src: "/screens/api-reference.png",
				alt: "The API reference: the sections of the JSON API, authentication with an API key and client examples.",
			},
		},
		{
			id: "desktop",
			tab: "Desktop",
			title: "The desktop app runs on its own",
			text: "The desktop app starts the server inside the window, with its own database on your machine, so it needs no terminal, no Docker and no network. Set VUKEM_TOOLS_URL to use a remote server instead. The release is shown in the corner of every screen.",
			command: "VUKEM_TOOLS_URL=https://tools.example.com",
		},
		{
			id: "docker",
			tab: "Docker",
			title: "One image for a VPS",
			text: "The container image holds the app and the conversion engines. Compose adds Postgres and ClamAV. The same image is what Railway builds.",
			command: "docker compose up -d --build --wait",
		},
	] satisfies Feature[],
};
