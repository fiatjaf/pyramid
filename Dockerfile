FROM golang:1.25 AS builder

# Install necessary tools
RUN apt-get update && \
    apt-get install -y musl-tools git curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY . .

# Install Just build tool
RUN curl -sL https://just.systems/install.sh | bash -s -- --to /usr/local/bin

# Install Templ version specified in go.mod
RUN TEMPL_VERSION=$(grep 'github.com/a-h/templ' go.mod | awk '{print $2}') && \
    go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}

# Build
RUN just build

# Final image
FROM ubuntu:latest

WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/pyramid-exe ./pyramid-exe

ENV HOST="0.0.0.0"
ENV PORT="3334"
ENV DATA_PATH="./data"

CMD ["./pyramid-exe"]