FROM node:25 AS tailwind-builder

WORKDIR /app
COPY . .

# Install dependencies
RUN npm install 

RUN npx tailwindcss -i base.css -o static/styles.css

FROM golang:1.25 AS builder

# Install necessary tools
RUN apt-get update && \
    apt-get install -y musl-tools git curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY . .
COPY --from=tailwind-builder /app/static ./static

# Install Templ version specified in go.mod
RUN TEMPL_VERSION=$(grep 'github.com/a-h/templ' go.mod | awk '{print $2}') && \
    go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

# Build
RUN templ generate
RUN VERSION=$(git describe --tags --exact-match 2>/dev/null || echo "$(git describe --tags --abbrev=0)-$(git rev-parse --short=8 HEAD)")
RUN CC=musl-gcc go build -tags=libsecp256k1 -ldflags="-X main.currentVersion=$VERSION -linkmode external -extldflags \"-static\"" -o ./pyramid-exe

# Final image
FROM ubuntu:latest

WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/pyramid-exe ./pyramid-exe

ENV HOST="0.0.0.0"
ENV PORT="3334"
ENV DATA_PATH="./data"

CMD ["./pyramid-exe"]