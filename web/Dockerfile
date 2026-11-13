FROM node:26.13.1-alpine
WORKDIR /app

COPY package*.json .npmrc ./
RUN npm ci

COPY . .
EXPOSE 5173
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0", "--port", "5173"]
