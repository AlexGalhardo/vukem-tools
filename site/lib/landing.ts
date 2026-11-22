/** Copy of the landing page (English only). Every claim here is backed by the catalogue or the code. */

export type Link = { label: string; href: string; external?: boolean };

export type Section = {
	/** The anchor: /#pdf. */
	id: string;
	/** The short name in the section dots. */
	nav: string;
	eyebrow: string;
	title: string;
	text: string;
	points: string[];
	links?: Link[];
	/** A command or a short block shown under the points. */
	code?: { label: string; lines: string[] };
	image: { src: string; alt: string; width: number; height: number };
};

const screen = (src: string, alt: string) => ({ src, alt, width: 1440, height: 900 });

export const landing = {
	title: "Vukem Tools: convert, compress and edit files on your own machine",
	description:
		"An open source, self-hosted file toolbox with a desktop app and a web app: PDF, image, video and audio tools, plus a CLI, a REST API and MCP for AI agents.",
	sectionsLabel: "Sections",
	hero: {
		id: "top",
		nav: "Start",
		eyebrow: "Open source · Self-hosted · Desktop and web",
		title: "Convert, compress and edit files on your own machine.",
		lead: "Vukem Tools is one toolbox for PDF, image, video and audio files, in the browser and in a desktop window. Your AI agents get the same tools from a CLI, a REST API and MCP.",
		download: { label: "Download", href: "https://github.com/AlexGalhardo/vukem-tools/releases", external: true },
		docs: { label: "Read the docs", href: "/docs/getting-started/quick-start/" },
		licence:
			"The code is MIT. Not for commercial use: several conversion engines are GPL or AGPL.",
		licenceLink: { label: "Every engine and its licence", href: "/docs/self-hosting/licences/" },
		image: screen(
			"/screens/catalogue-search.webp",
			"The Vukem Tools catalogue with the search field filled in with mp4: five video tools match, each with its input and output formats and a Ready badge.",
		),
	},
	sections: [
		{
			id: "pdf",
			nav: "PDF",
			eyebrow: "PDF · 24 tools",
			title: "Everything you do to a PDF, in one place.",
			text: "Merge, split, rotate and crop pages. Compress, repair, protect and unlock a file. Run OCR, fill forms and sign. None of it needs an account on someone else's server.",
			points: [
				"Page tools: merge, split, extract, remove, organise, rotate and crop.",
				"Make a PDF from images, or from Word, PowerPoint and Excel files.",
				"Go the other way: PDF to JPG or PNG, to text or Markdown, or to PDF/A.",
				"Add page numbers or a watermark, compare two files, and set a password.",
			],
			links: [{ label: "PDF crop guide", href: "/docs/guide/pdf-crop/" }],
			image: screen(
				"/screens/pdf-crop.webp",
				"The Crop a PDF page: the PDF tools list in the sidebar, a drop zone, a report.pdf file marked ready, an options panel and the Crop a PDF button.",
			),
		},
		{
			id: "images",
			nav: "Images",
			eyebrow: "Images and sprites",
			title: "Turn pixel art into clean vectors.",
			text: "The pixel art tracer writes one SVG path per colour region, with no blurring or smoothing. Around it sit the tools a game or web project asks for.",
			points: [
				"Convert and compress images between PNG, JPEG, WebP, AVIF, GIF, TIFF and BMP.",
				"Split a sprite sheet into frames and an atlas, or pack frames into one sheet.",
				"Pack files into ZIP, TAR.GZ, TAR or GZ, and extract them again.",
			],
			image: screen(
				"/screens/pixel-art-to-svg.webp",
				"The Pixel art to SVG page after a conversion: sprite.png went from 139 bytes to a 1.7 KiB SVG, with a Download button and a countdown before the result is deleted.",
			),
		},
		{
			id: "media",
			nav: "Video and audio",
			eyebrow: "Video and audio",
			title: "Convert, cut and download media.",
			text: "Video and audio conversion runs on ffmpeg with a timeout and a size limit. You choose the container, the codec and the quality, or keep a sensible default.",
			points: [
				"Convert video between MP4, WebM and MKV, and audio between MP3, WAV, FLAC, Ogg and AAC.",
				"Cut, crop or split a recording into clips, compress a file, or make a GIF.",
				"Download a video from YouTube, Instagram, X or TikTok, as video or audio only. You are responsible for what you download.",
			],
			image: screen(
				"/screens/video-convert.webp",
				"The Convert a video page after a conversion: clip.mp4 finished at 4% smaller, with a Download button and the time left before the result is deleted.",
			),
		},
		{
			id: "dev",
			nav: "Developer tools",
			eyebrow: "Developer tools · in your browser",
			title: "Small utilities that never upload a thing.",
			text: "These tools run in the page. What you paste stays in the tab: there is no request to send it anywhere.",
			points: [
				"Format and validate JSON, check it against a JSON Schema, and convert between JSON, YAML, CSV and XML.",
				"Diff two texts, hash and HMAC, decode a JWT, and encode Base64.",
				"Generate UUIDs and strong passwords.",
				"Count tokens and estimate the cost of a prompt.",
			],
			image: screen(
				"/screens/json-format.webp",
				"The Format and validate JSON page: a compact JSON object in the input, a Valid JSON message, and the pretty-printed result below with a Copy button.",
			),
		},
		{
			id: "agents",
			nav: "Agents",
			eyebrow: "CLI · REST API · MCP",
			title: "Your agent uses the same tools you do.",
			text: "Every tool that runs on the server is also a CLI command, a REST endpoint and an MCP tool, under the same name. An API key carries only the abilities you gave it.",
			points: [
				"The CLI uploads the file, runs the tool on your server and downloads the result.",
				"The REST API has a generated OpenAPI reference, served by the app at /docs.",
				"The MCP server speaks JSON-RPC on stdin and stdout, or over local HTTP.",
			],
			code: {
				label: "In a terminal",
				lines: ["vukem-tools tools", "vukem-tools pdf crop --input in.pdf --output out.pdf", "vukem-tools mcp"],
			},
			links: [
				{ label: "Agents overview", href: "/docs/agents/overview/" },
				{ label: "REST API", href: "/docs/api/overview/" },
				{ label: "MCP", href: "/docs/mcp/overview/" },
			],
			image: screen(
				"/screens/api-reference.webp",
				"The generated API reference: the sections of the JSON API in the sidebar, authentication with an API key as a bearer token, and client examples in shell, Ruby, Node.js, PHP and Python.",
			),
		},
		{
			id: "self-host",
			nav: "Self-host",
			eyebrow: "Desktop · Docker · MIT code",
			title: "Run it on your machine, or on your own server.",
			text: "The desktop app starts the whole server inside its window, with a database on your computer. For a server, the Docker image bundles the conversion engines. Results are deleted five minutes after they are made.",
			points: [
				"Desktop installers for Windows, Linux and macOS are on the GitHub releases page.",
				"Or run the Docker Compose stack: the app, Postgres and a malware scanner.",
				"Not for commercial use: FFmpeg, Ghostscript and Poppler are GPL or AGPL, and the Licences page lists every engine.",
			],
			links: [
				{ label: "Installation", href: "/docs/getting-started/installation/" },
				{ label: "Licences", href: "/docs/self-hosting/licences/" },
			],
			image: screen(
				"/screens/licenses.webp",
				"The Licences page of the app: Vukem Tools is MIT, a warning explains that FFmpeg, Ghostscript and Poppler carry copyleft licences, and a table lists everything the service runs.",
			),
		},
	] satisfies Section[],
	closing: {
		id: "start",
		nav: "Get started",
		title: "Start with one file.",
		text: "Download the desktop app, or run it with Docker, then crop a PDF to see it work. The code is on GitHub.",
		primary: { label: "Download from GitHub", href: "https://github.com/AlexGalhardo/vukem-tools/releases", external: true },
		secondary: { label: "Quick start", href: "/docs/getting-started/quick-start/" },
		tertiary: { label: "Source code", href: "https://github.com/AlexGalhardo/vukem-tools", external: true },
		note: "Open source under MIT. Not for commercial use: several conversion engines are GPL or AGPL.",
	},
};
