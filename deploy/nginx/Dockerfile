FROM node:26.13.1-alpine AS web-builder
WORKDIR /src/web

COPY web/package*.json web/.npmrc ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM nginx:1.27-alpine

COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=web-builder /src/web/dist /usr/share/nginx/html

EXPOSE 80
