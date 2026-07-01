# ---- Web build ----
FROM node:20-alpine AS web-build
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ---- Go build ----
FROM golang:1.22-alpine AS go-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /app/web/dist ./cmd/aether-server/frontend-dist
RUN go build -o /aether-server ./cmd/aether-server

# ---- Final image ----
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
RUN addgroup -S aether && adduser -S aether -G aether
COPY --from=go-build /aether-server /usr/local/bin/
USER aether
EXPOSE 8080
CMD ["aether-server"]
