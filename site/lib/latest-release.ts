export const IMAGE = "ghcr.io/alexgalhardo/vukem-tools";

/**
 * The command that starts the stack from this repository: Postgres, ClamAV and the app on port 3000.
 * Set APP_KEY in the environment first. The image tag latest moves only on a stable release.
 */
export function dockerRunCommand(): string {
	return ['export APP_KEY="base64:$(openssl rand -base64 32)"', "docker compose up -d --build --wait"].join("\n");
}
