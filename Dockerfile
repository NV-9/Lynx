# Stage 1: Build frontend assets (Tailwind CSS + HTMX)
FROM node:20-alpine AS frontend
WORKDIR /app
RUN --mount=type=cache,target=/root/.npm \
    npm install -g tailwindcss@3
COPY tailwind.config.js .
COPY static/ static/
RUN tailwindcss -i static/input.css -o static/styles.css --minify && \
    wget -qO static/htmx.min.js https://unpkg.com/htmx.org@1.9.12/dist/htmx.min.js

# Stage 2: Build Go binary
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/static/styles.css  static/styles.css
COPY --from=frontend /app/static/htmx.min.js static/htmx.min.js
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o lynx .

# Stage 3: Run
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/lynx .
VOLUME /data
EXPOSE 8080
ENV PORT=8080
ENV BASE_URL=http://localhost:8080
ENTRYPOINT ["./lynx"]
