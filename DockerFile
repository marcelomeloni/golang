FROM golang:1.23-bookworm AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM debian:bookworm-slim

# Chromium e dependências mínimas para o chromedp funcionar em container
RUN apt-get update && apt-get install -y \
    chromium \
    fonts-liberation \
    libnss3 \
    libatk-bridge2.0-0 \
    libgtk-3-0 \
    libxss1 \
    libasound2 \
    --no-install-recommends \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/server .
COPY templates/ templates/

# chromedp usa esta variável para localizar o binário
ENV CHROME_BIN=/usr/bin/chromium

EXPOSE 8080
CMD ["./server"]
