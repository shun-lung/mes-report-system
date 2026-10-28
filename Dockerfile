# 報工系統：前端建置 + Go 編譯 → 單一執行映像（API 一併提供網頁）

FROM node:20-alpine AS web
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS go
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/demo ./cmd/dbsync ./cmd/setpass

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -H mes
WORKDIR /app
COPY --from=go /out/ /app/bin/
COPY --from=web /src/dist /app/dist
ENV STATIC_DIR=/app/dist GIN_MODE=release
USER mes
# 雲端主機以 PORT 指定埠號；本機執行可用 -e PORT=8080
CMD ["/app/bin/api"]
