# syntax=docker/dockerfile:1

FROM node:25 AS tailwind-builder

WORKDIR /app

# install dependencies
COPY package.json ./
RUN --mount=type=cache,target=/root/.npm npm install

COPY . .
RUN npx tailwindcss -i base.css -o static/styles.css

FROM golang:1.26 AS builder

# install necessary tools
RUN apt-get update && \
    apt-get install -y musl-tools git curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download && \
    TEMPL_VERSION=$(grep 'github.com/a-h/templ' go.mod | awk '{print $2}') && \
    go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

COPY . .
COPY --from=tailwind-builder /app/static ./static

# build
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    templ generate && \
    VERSION=$(git describe --tags --exact-match 2>/dev/null || echo "$(git describe --tags --abbrev=0)-$(git rev-parse --short=8 HEAD)") && \
    CC=musl-gcc go build -tags=libsecp256k1 -ldflags="-X main.currentVersion=$VERSION -linkmode external -extldflags \"-static\"" -o ./pyramid-exe

# final image
FROM ubuntu:latest

# runtime dependencies:
#   - git: required by the grasp (NIP-34) feature, which shells out to
#     `git init --bare`, `git upload-pack`, `git receive-pack`, etc.
#   - ca-certificates: required for outbound HTTPS — ACME/autocert (Let's Encrypt),
#     GitHub API calls (self-update, embedded LiveKit release fetch), and
#     federated relay sync.
#   - tzdata: correct local-time formatting in templates.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git \
        ca-certificates \
        tzdata && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# copy the built binary from the builder stage
COPY --from=builder /app/pyramid-exe ./pyramid-exe

ENV HOST="0.0.0.0"
ENV PORT="3334"
ENV DATA_PATH="./data"
ENV NO_AUTO_UPDATES="true"

CMD ["./pyramid-exe"]
