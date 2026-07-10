FROM ghcr.io/hybridgroup/opencv:4.10.0

WORKDIR /app

# go.mod pide >= 1.24; la imagen OpenCV trae Go 1.23 → descargar toolchain automáticamente
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -o /usr/local/bin/tower-scraper cmd/scraper/main.go

# --- FIX: Actualizar llaves GPG de Debian Bullseye ---
# Esto evita el error de "invalid signature" al hacer apt-get update
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update --allow-insecure-repositories || true \
    && apt-get install -y --allow-unauthenticated debian-archive-keyring \
    && apt-get update
# -----------------------------------------------------

# Solo Chromium (no Firefox/WebKit) + reintentos ante timeouts de apt en redes lentas.
RUN set -eux; \
    for attempt in 1 2 3 4 5; do \
      apt-get update --fix-missing || true; \
      if go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6100.0 install --with-deps chromium; then \
        exit 0; \
      fi; \
      echo "playwright install: reintento ${attempt}/5 en 30s..."; \
      sleep 30; \
    done; \
    exit 1

EXPOSE ${APP_PORT}
ENV MCP_TRANSPORT=sse
ENV APP_PORT=${APP_PORT}

CMD ["tower-scraper"]