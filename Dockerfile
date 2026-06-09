FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/bot ./cmd/bot

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
RUN adduser -D -H app
WORKDIR /app
COPY --from=build /out/bot /app/bot
USER app
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1
CMD ["/app/bot"]
