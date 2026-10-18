// Conventional Commits 1.0.0 (https://www.conventionalcommits.org/en/v1.0.0/).
// Scopes mirror .claude/rules/commits.md.
module.exports = {
	extends: ["@commitlint/config-conventional"],
	rules: {
		"scope-enum": [
			2,
			"always",
			[
				"auth",
				"apikeys",
				"jobs",
				"upload",
				"storage",
				"image",
				"video",
				"audio",
				"pdf",
				"pixelsvg",
				"downloader",
				"editor",
				"bot",
				"api",
				"ui",
				"infra",
				"ci",
				"deps",
				"repo",
				"docs",
				"release",
			],
		],
	},
};
