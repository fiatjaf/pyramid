FROM golang:1.24.2 AS builder
WORKDIR /app
COPY . .
RUN go build -o pyramid .

FROM ubuntu:latest
COPY --from=builder /app/pyramid /app/
ENV DATABASE_PATH="/app/db"
ENV USERDATA_PATH="/app/users.json"
CMD ["/app/pyramid"]
