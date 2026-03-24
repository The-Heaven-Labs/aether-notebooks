FROM node:20-alpine
WORKDIR /app
COPY package.json ./
RUN npm install --production=false
COPY tsconfig.json ./
COPY src ./src
RUN npm run build
CMD ["node", "dist/index.js"]
