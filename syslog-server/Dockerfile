ARG BUILD_FROM=alpine:3.20
FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o syslog-server .

FROM ${BUILD_FROM}

RUN apk add --no-cache tzdata ca-certificates

WORKDIR /app
COPY --from=builder /build/syslog-server /app/syslog-server

EXPOSE 514/udp 8099

ENTRYPOINT ["/app/syslog-server"]
