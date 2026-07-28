# Stage 1: cache modules
FROM golang:1.24-bookworm AS modules
COPY go.mod go.sum /modules/
WORKDIR /modules
RUN go mod download

# Stage 2: build + playwright CLI
FROM golang:1.24-bookworm AS builder
COPY --from=modules /go/pkg /go/pkg
COPY . /workdir
WORKDIR /workdir

RUN PWGO_VER=$(grep -oE "mxschmitt/playwright-go v[^ ]+" go.mod | awk '{print $2}') \
    && go install "github.com/mxschmitt/playwright-go/cmd/playwright@${PWGO_VER}"

RUN CGO_ENABLED=0 GOOS=linux go build -o /tower-scraper cmd/scraper/main.go

# Stage 3: runtime con Chromium para Google Maps MCP
FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

COPY --from=builder /go/bin/playwright /usr/local/bin/playwright
COPY --from=builder /tower-scraper /usr/local/bin/tower-scraper

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

EXPOSE 8080
ENV MCP_TRANSPORT=sse
ENV APP_PORT=8080

CMD ["tower-scraper"]
