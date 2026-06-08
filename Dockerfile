FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/bot ./cmd/bot

FROM alpine:3.20
RUN adduser -D -H app
WORKDIR /app
COPY --from=build /out/bot /app/bot
USER app
EXPOSE 8080
CMD ["/app/bot"]
