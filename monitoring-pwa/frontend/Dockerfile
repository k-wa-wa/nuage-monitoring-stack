# 文脈はリポジトリ直下 (@nuage/ui = packages/nuage-ui を取り込むため)。
# docker build -f monitoring-pwa/frontend/Dockerfile .
FROM oven/bun:1-alpine AS builder
WORKDIR /app
# 依存はリポジトリ直下の bun workspace でまとめて入れる
COPY package.json bun.lock ./
COPY packages/nuage-ui/package.json packages/nuage-ui/
COPY monitoring-pwa/frontend/package.json monitoring-pwa/frontend/
RUN bun install --frozen-lockfile
COPY packages/nuage-ui packages/nuage-ui
COPY monitoring-pwa/frontend monitoring-pwa/frontend
RUN cd monitoring-pwa/frontend && bun run build

FROM nginx/nginx-unprivileged:1.31.9-alpine
USER root
RUN apk --no-cache upgrade brotli-libs
USER 101
COPY --from=builder /app/monitoring-pwa/frontend/dist /usr/share/nginx/html
COPY monitoring-pwa/frontend/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
USER 101
CMD ["nginx", "-g", "daemon off;"]
