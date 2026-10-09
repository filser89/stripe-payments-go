FROM golang:1.27.2-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /service ./cmd/service

FROM alpine:3.23.4
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app
WORKDIR /app
COPY --from=build /service /app/service
COPY db/migrations /app/db/migrations
USER app
EXPOSE 8080
ENTRYPOINT ["/app/service"]
