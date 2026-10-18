# Production image: Vite assets, a static Go binary, the conversion engines,
# a non-root runtime.
#
# The runtime is Debian rather than Alpine, which the vukem scaffold uses, for
# three reasons that are all about the engines:
#
#   * yt-dlp ships one official Linux binary, built against glibc. On musl it
#     does not start at all.
#   * Debian's ffmpeg is the GPL build, with the encoders this project needs.
#     Alpine's is built without several of them.
#   * prlimit comes from util-linux, which is how every engine gets its memory,
#     CPU and file-size limits (internal/engine/sandbox_linux.go explains why
#     Go cannot apply them itself).
#
# That costs image size. docs/deployment.md records the figure and why it is
# accepted.
#
# https://vukem.dev/docs/deployment/docker-and-railway

FROM oven/bun:1.4.3-alpine AS assets
WORKDIR /src
COPY package.json bun.lock* ./
RUN bun install --frozen-lockfile
COPY . .
RUN bun run build

FROM golang:1.27.2-alpine3.24 AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /src/resources/public/build resources/public/build
# Same tags as vukem build: .buildtags lists them (one per line or
# comma-separated, "#" comments); --minimal apps put vukem_nosqlite there.
RUN tags="$(sed 's/#.*//' .buildtags 2>/dev/null | tr -s ', \t\r\n' ',' | sed 's/^,//; s/,$//')" \
	&& go build -trimpath -ldflags="-s -w" -tags "$tags" -o /out/app ./cmd/app

# yt-dlp is fetched in its own stage so the checksum failure is a build failure
# and the download tools never reach the runtime image.
FROM debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f AS ytdlp
ARG YTDLP_VERSION=2026.08.19
ARG YTDLP_SHA256=58162f9bfdc27458ea47bfcb311cf47028f17d8154a8bf7d689861d46399230a
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl \
	&& curl -fsSLo /tmp/yt-dlp \
		"https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_linux" \
	&& echo "${YTDLP_SHA256}  /tmp/yt-dlp" | sha256sum -c - \
	&& chmod 0755 /tmp/yt-dlp

FROM debian:trixie-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f

# Every engine is pinned to the exact Debian version, never a distro default
# and never "latest" (.claude/rules/engines.md). A pin can leave the mirror
# when Debian supersedes it; docs/deployment.md says how to refresh one and how
# to build against snapshot.debian.org meanwhile.
#
#   ffmpeg         video and audio, GPL-3.0 build
#   libvips-tools  raster images, the primary image engine
#   ghostscript    PDF, AGPL-3.0-or-later
#   poppler-utils  PDF text and page extraction
#   qpdf           PDF structure: split, merge, rotate, encrypt
#   clamav         uploaded-file scanning
#   util-linux     prlimit, which applies every engine's resource limits
ARG FFMPEG_VERSION=7:7.1.5-0+deb13u1
ARG LIBVIPS_VERSION=8.16.1-1+deb13u2
ARG GHOSTSCRIPT_VERSION=10.05.1~dfsg-1+deb13u3
ARG POPPLER_VERSION=25.03.0-5+deb13u4
ARG QPDF_VERSION=12.2.0-1
ARG CLAMAV_VERSION=1.4.3+dfsg-1
ARG UTIL_LINUX_VERSION=2.41.5-0+deb13u1
# A security update Debian published after the pinned base image was built, so
# the image ships it rather than waiting for the next base digest. Installing
# it by name keeps every other package at the digest's version: a blanket
# apt-get upgrade would unpin the whole image.
ARG LIBLZMA_VERSION=5.8.1-1+deb13u2

RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		ca-certificates \
		tzdata \
		"ffmpeg=${FFMPEG_VERSION}" \
		"libvips-tools=${LIBVIPS_VERSION}" \
		"ghostscript=${GHOSTSCRIPT_VERSION}" \
		"poppler-utils=${POPPLER_VERSION}" \
		"qpdf=${QPDF_VERSION}" \
		"clamav=${CLAMAV_VERSION}" \
		"util-linux=${UTIL_LINUX_VERSION}" \
		"liblzma5=${LIBLZMA_VERSION}" \
	&& rm -rf /var/lib/apt/lists/* \
	&& useradd --system --no-create-home --uid 10001 app \
	&& mkdir -p /app/storage /app/work \
	&& chown app:app /app/storage /app/work

COPY --from=ytdlp /tmp/yt-dlp /usr/local/bin/yt-dlp

WORKDIR /app
COPY --from=build /out/app /app/app
USER app

# The platform's PORT overrides this one; HOST=0.0.0.0 makes the server
# reachable from outside the container. Pass secrets at runtime, never here.
#
# ENGINE_WORKDIR is on the image's own filesystem rather than /tmp: a
# conversion writes large intermediate files, and /tmp is a tmpfs on several
# platforms, which would put them in RAM.
ENV APP_ENV=production \
	HOST=0.0.0.0 \
	PORT=3000 \
	LOG_FORMAT=json \
	ENGINE_WORKDIR=/app/work
EXPOSE 3000

# The app logs every engine's resolved path and version at startup and refuses
# to start when a required one is missing, so a broken image fails the deploy
# rather than the first conversion that needs it.
ENTRYPOINT ["/app/app"]
