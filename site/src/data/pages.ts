export type DocPage = {
	slug: string;
	title: string;
	section: string;
	summary: string;
	paragraphs: string[];
};

export const pages: DocPage[] = [
	{
		slug: "",
		title: "Documentation",
		section: "Getting started",
		summary: "How to convert a file with Vukem Tools, from the browser, the desktop window, or an agent.",
		paragraphs: [
			"Vukem Tools converts files. The same catalogue is on the web app, in the desktop window, and on the command line, the HTTP API, and MCP.",
			"Start with installation, then crop a PDF from an agent if that is the job in front of you.",
		],
	},
	{
		slug: "introduction",
		title: "Introduction",
		section: "Getting started",
		summary: "What the toolbox does and what it leaves alone.",
		paragraphs: [
			"Each tool reads a file, writes another file, and stops. There is no timeline, no caption track, and no project you have to save.",
			"Login is required for the web app. An API key carries the abilities of the tools it may call.",
		],
	},
	{
		slug: "installation",
		title: "Installation",
		section: "Getting started",
		summary: "Run the web app, or open the desktop window that hosts it.",
		paragraphs: [
			"The web app is one container image, ghcr.io/alexgalhardo/vukem-tools. The variables it needs are in the deployment document.",
			"Desktop builds are a Windows .exe, a Linux .AppImage, and a macOS .dmg. The window opens the web UI. It does not reimplement a tool.",
		],
	},
	{
		slug: "quick-start",
		title: "Quick start",
		section: "Getting started",
		summary: "Crop every page of a PDF from the command line.",
		paragraphs: [
			"vukem-tools pdf crop --input in.pdf --output out.pdf --left 36 --bottom 36 --right 36 --top 36",
			"Leave the page range empty and every page is cropped. Margins are PDF points. A crop that trims nothing is refused.",
		],
	},
	{
		slug: "mcp",
		title: "MCP",
		section: "MCP",
		summary: "One MCP server over the same catalogue the CLI and the HTTP API use.",
		paragraphs: [
			"Run vukem-tools mcp and speak newline-delimited JSON-RPC on stdin. tools/list returns every conversion tool this build registered.",
			"tools/call with the name pdf-crop crops every page when the arguments omit a page range. Pass input, output, and at least one non-zero margin.",
		],
	},
	{
		slug: "mcp/connect",
		title: "Connect an MCP client",
		section: "MCP",
		summary: "Point the client at the vukem-tools mcp command.",
		paragraphs: [
			"The server is the binary. A client starts it and writes one JSON object per line. Errors come back as JSON, not as a stack trace.",
			"The same binary serves HTTP with vukem-tools serve. GET /tools lists the catalogue. POST /pdf/crop runs the crop.",
		],
	},
	{
		slug: "mcp/tools",
		title: "MCP tools",
		section: "MCP",
		summary: "Discovery lists the conversion tools, not the browser-only helpers.",
		paragraphs: [
			"Image, video, and download tools stay hidden when their engine was not resolved. That is the same rule the HTTP catalogue uses.",
			"pdf-crop is the tool an agent calls to crop every page of a PDF.",
		],
	},
	{
		slug: "api",
		title: "HTTP API",
		section: "API",
		summary: "The agent HTTP surface and the product API share one catalogue.",
		paragraphs: [
			"GET /api/v1/tools is the product catalogue, behind a login and an ability. The local agent server exposes GET /tools for the same names.",
			"POST /pdf/crop on the agent server takes a JSON body with input, output, and margins, and returns the cropped file path, the page count, and the size.",
		],
	},
	{
		slug: "api/endpoints",
		title: "Endpoints",
		section: "API",
		summary: "The calls an agent needs for discovery and for cropping a PDF.",
		paragraphs: [
			"GET /tools returns {\"tools\":[{\"name\",\"title\",\"summary\"}]}.",
			"POST /pdf/crop returns 200 with the crop result, or 422 with {\"error\":\"...\"} when the input, the output, or qpdf fails.",
			"The product route POST /api/v1/tools/pdf-crop still enqueues the same crop on the worker. Empty pages means every page.",
		],
	},
	{
		slug: "concepts/catalog",
		title: "One catalogue",
		section: "Concepts",
		summary: "A tool name means the same thing in the browser, the window, the CLI, HTTP, and MCP.",
		paragraphs: [
			"pdf-crop is the name. The web page is /tools/pdf-crop. The desktop window opens that page. The agent calls the same name.",
		],
	},
	{
		slug: "concepts/engines",
		title: "Engines",
		section: "Concepts",
		summary: "Each conversion engine is a subprocess with a timeout and a resource limit.",
		paragraphs: [
			"qpdf crops PDF pages by rewriting the crop box. ffmpeg, libvips, and the other engines stay pinned in the image. The desktop window does not bundle them.",
		],
	},
	{
		slug: "guide/convert",
		title: "Convert a file",
		section: "Guide",
		summary: "Pick a tool, give it a file, take the result.",
		paragraphs: [
			"The web form is the same screen for every tool. The desktop window loads that screen. An agent skips the form and calls the tool by name.",
		],
	},
	{
		slug: "guide/pdf-crop",
		title: "Crop a PDF",
		section: "Guide",
		summary: "Trim the same margin from every page, or from a page range.",
		paragraphs: [
			"Margins are left, bottom, right, and top, in points. An empty page list crops every page. At least one margin must be greater than zero.",
		],
	},
	{
		slug: "agents/overview",
		title: "Agents",
		section: "Agents",
		summary: "Tell an agent to use vukem-tools and name the tool.",
		paragraphs: [
			"The sentence “use vukem-tools and crop all pages of this PDF” maps to pdf-crop with no page range. The CLI, the HTTP server, and MCP all accept that call.",
		],
	},
	{
		slug: "agents/cli",
		title: "Command line",
		section: "Agents",
		summary: "vukem-tools tools, pdf crop, mcp, and serve.",
		paragraphs: [
			"vukem-tools tools prints the catalogue as JSON. pdf crop reads --input and --output. mcp speaks JSON-RPC. serve listens on 127.0.0.1:8787 unless --addr says otherwise.",
		],
	},
	{
		slug: "prompts/overview",
		title: "Prompts",
		section: "Prompts",
		summary: "A prompt that names the tool and the file.",
		paragraphs: [
			"Use vukem-tools. Crop every page of the PDF at this path. Keep a 36-point margin on each edge. Write the result next to the original.",
			"The same instruction works in any client that can run the CLI or speak MCP.",
		],
	},
	{
		slug: "self-hosting/docker",
		title: "Docker",
		section: "Self-hosting",
		summary: "One image, configured with environment variables.",
		paragraphs: [
			"Build the Dockerfile or pull ghcr.io/alexgalhardo/vukem-tools. Set APP_KEY, APP_URL, DATABASE_URL, and the mail and scanner variables documented in docs/deployment.md.",
		],
	},
	{
		slug: "self-hosting/railway",
		title: "Railway",
		section: "Self-hosting",
		summary: "Railway builds the same Dockerfile.",
		paragraphs: [
			"Point the service at this repository and set the variables from the deployment document. The suggested hostname for the app is app.tools.vukem.dev. The docs site stays on tools.vukem.dev.",
		],
	},
	{
		slug: "self-hosting/desktop",
		title: "Desktop updates",
		section: "Self-hosting",
		summary: "The launcher moves forward only.",
		paragraphs: [
			"An update is accepted only when its version is strictly newer than the one installed. An artifact whose SHA-256 does not match the feed is deleted and is not installed.",
			"Signature checks wait for an owner public key. The checksum check is not turned off in its place.",
		],
	},
	{
		slug: "reference/cli",
		title: "CLI reference",
		section: "Reference",
		summary: "Commands and the JSON they print.",
		paragraphs: [
			"tools writes {\"tools\":[...]}. pdf crop writes {\"output\",\"cropped\",\"bytes\"}. A failure writes {\"error\":\"...\"} on stderr and exits non-zero.",
		],
	},
	{
		slug: "reference/configuration",
		title: "Configuration",
		section: "Reference",
		summary: "Environment the desktop window and the server read.",
		paragraphs: [
			"VUKEM_TOOLS_URL is the origin the window opens. VUKEM_TOOLS_TOOL is the catalogue name, default pdf-crop. QPDF_PATH points at qpdf when it is not on PATH.",
			"The web app reads APP_KEY, APP_URL, APP_ENV, DATABASE_URL, MAIL_MAILER, and CLAMAV_ADDR. See docs/deployment.md.",
		],
	},
	{
		slug: "glossary",
		title: "Glossary",
		section: "Glossary",
		summary: "Words this toolbox uses.",
		paragraphs: [
			"A tool is a named conversion. A catalogue is the list of tools this build can run. A margin is a distance in PDF points, 72 to the inch. An empty page range means every page.",
		],
	},
	{
		slug: "contributing",
		title: "Contributing",
		section: "Contributing",
		summary: "How a change is reviewed.",
		paragraphs: [
			"Reviews use three severities: Important, Nit, and Pre-existing. Important blocks the merge. A nit is a small fix. Pre-existing is named and left for its own change.",
			"CODEOWNERS covers the review rules, the workflows, the image, and the desktop packaging.",
		],
	},
	{
		slug: "troubleshooting",
		title: "Troubleshooting",
		section: "Troubleshooting",
		summary: "The failures the crop and the window hit first.",
		paragraphs: [
			"qpdf is not installed: set QPDF_PATH or put qpdf on PATH. A crop of nothing is refused: set a margin. The desktop window shows the web app, so the app has to be running at VUKEM_TOOLS_URL.",
		],
	},
];

export function pageBySlug(slug: string): DocPage | undefined {
	return pages.find((page) => page.slug === slug);
}

export function sections(): string[] {
	const seen: string[] = [];
	for (const page of pages) {
		if (!seen.includes(page.section)) {
			seen.push(page.section);
		}
	}
	return seen;
}
