FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/nats-console-api ./cmd/server

FROM alpine:3.22
WORKDIR /service
COPY --from=builder /bin/nats-console-api /usr/local/bin/nats-console-api
EXPOSE 8080
CMD ["nats-console-api"]
