# Production image: Vite assets, a static Go binary, a small non-root runtime.
# Railway, Fly, Render and plain Docker build it as is:
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

FROM alpine:3.24.2
RUN apk add --no-cache ca-certificates tzdata \
	&& adduser -D -H -u 10001 app \
	&& mkdir -p /app/storage \
	&& chown app:app /app/storage
WORKDIR /app
COPY --from=build /out/app /app/app
USER app
# The platform's PORT overrides this one; HOST=0.0.0.0 makes the server
# reachable from outside the container. Pass secrets at runtime, never here.
ENV APP_ENV=production HOST=0.0.0.0 PORT=3000 LOG_FORMAT=json
EXPOSE 3000
ENTRYPOINT ["/app/app"]
