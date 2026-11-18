/** Copy of the landing page (English only). */

export type Feature = {
	id: string;
	tab: string;
	title: string;
	text: string;
	command: string;
	image: { src: string; alt: string };
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
		placeholderNote: "Placeholder images stand in until screenshots of the tools are added.",
	},
	features: [
		{
			id: "crop",
			tab: "PDF crop",
			title: "Crop every page",
			text: "Trim the same margin from every page of a PDF. The web form, the CLI, the HTTP API, and MCP call the same crop.",
			command: "vukem-tools pdf crop --input in.pdf --output out.pdf",
			image: {
				src: "/placeholders/export.svg",
				alt: "Placeholder for the PDF crop tool.",
			},
		},
		{
			id: "catalogue",
			tab: "Catalogue",
			title: "One list of tools",
			text: "Image, audio, video, PDF, archives, and sprite sheets are entries in one catalogue. A browser-only tool is marked and does not need the server pipeline.",
			command: "vukem-tools tools",
			image: {
				src: "/placeholders/cuts.svg",
				alt: "Placeholder for the tool catalogue.",
			},
		},
		{
			id: "agents",
			tab: "Agents",
			title: "Agents use the same names",
			text: "An agent lists the tools, then crops a PDF through the CLI, a local HTTP server, or MCP. The tool name is pdf-crop on every surface.",
			command: "vukem-tools mcp",
			image: {
				src: "/placeholders/agents.svg",
				alt: "Placeholder for an agent calling vukem-tools.",
			},
		},
		{
			id: "desktop",
			tab: "Desktop",
			title: "The window is the web app",
			text: "The Windows, Linux, and macOS downloads open the web app. They do not ship a second copy of a tool. The release is shown in the sidebar of the window.",
			command: "VUKEM_TOOLS_URL=http://127.0.0.1:3000",
			image: {
				src: "/placeholders/compositing.svg",
				alt: "Placeholder for the desktop window.",
			},
		},
		{
			id: "docker",
			tab: "Docker",
			title: "One image for a VPS",
			text: "The container image holds the app and the conversion engines. Compose adds Postgres and ClamAV. The same image is what Railway builds.",
			command: "docker compose up -d --build --wait",
			image: {
				src: "/placeholders/captions.svg",
				alt: "Placeholder for the Docker stack.",
			},
		},
	] satisfies Feature[],
};
