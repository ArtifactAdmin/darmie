# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o darmie .

# Runtime stage
FROM alpine:3.21

WORKDIR /app

COPY --from=builder /app/darmie ./
COPY --from=builder /app/static ./static

# Persist the SQLite database outside the container
VOLUME ["/data"]

EXPOSE 8080

ENTRYPOINT ["./darmie", "-db", "/data/darmie.db"]
