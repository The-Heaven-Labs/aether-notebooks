# ---- Go build ----
FROM golang:1.22-alpine AS go-build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /hnb-server ./cmd/hnb-server

# ---- Web build ----
FROM node:20-alpine AS web-build
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# ---- Final image ----
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=go-build /hnb-server /usr/local/bin/
COPY --from=web-build /app/web/dist /var/www/hnb
EXPOSE 8080
CMD ["hnb-server"]
