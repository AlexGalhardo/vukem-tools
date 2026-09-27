// Conventional Commits 1.0.0 (https://www.conventionalcommits.org/en/v1.0.0/).
module.exports = {
	extends: ["@commitlint/config-conventional"],
	rules: {
		"scope-enum": [
			2,
			"always",
			["backend", "frontend", "telegram", "infra", "setups", "ci", "docs", "deps", "release"],
		],
	},
};
