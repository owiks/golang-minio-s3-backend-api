FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go run ./tools/openapi -check && CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./src

FROM alpine:3.24 AS production
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=build /out/api /app/api
COPY LICENSE /app/
USER 10001:10001
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8081/health || exit 1
ENTRYPOINT ["/app/api"]
