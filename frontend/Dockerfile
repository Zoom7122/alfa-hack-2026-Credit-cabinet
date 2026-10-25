# syntax=docker/dockerfile:1

FROM node:24.22-alpine AS build
WORKDIR /app

COPY package.json package-lock.json .npmrc ./
RUN npm ci --ignore-scripts --no-audit --no-fund

COPY . .
# true — ответы API отдаёт MSW в браузере (бэкенда пока нет); false — ходить в настоящий /api
ARG VITE_API_MOCK=true
ENV VITE_API_MOCK=$VITE_API_MOCK
# true — ответы data-collector тоже отдаёт MSW; false — ходить в живой сервис через /collector
ARG VITE_COLLECTOR_MOCK=true
ENV VITE_COLLECTOR_MOCK=$VITE_COLLECTOR_MOCK
RUN npm run build

# ---------- раздача ----------
FROM nginx:1.28-alpine
# куда nginx проксирует /api: схема, хост и порт без пути
ENV API_UPSTREAM=http://api:8080
# куда nginx проксирует /collector (префикс срезается)
ENV COLLECTOR_UPSTREAM=http://data-collector:8081
# DNS для резолва upstream; 127.0.0.11 — встроенный DNS Docker
ENV NGINX_RESOLVER=127.0.0.11

COPY nginx/snippets/ /etc/nginx/snippets/
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -qO- http://127.0.0.1/healthz || exit 1
