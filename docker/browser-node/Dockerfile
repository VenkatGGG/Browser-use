ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
ARG TARGETOS=linux
ARG TARGETARCH

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/node-agent ./cmd/node-agent

FROM debian:bookworm-slim

ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      gnupg \
      wget \
      xvfb \
      fonts-liberation && \
    if [ "${TARGETARCH}" = "amd64" ]; then \
      wget -qO- https://dl.google.com/linux/linux_signing_key.pub | gpg --dearmor > /usr/share/keyrings/google-linux.gpg && \
      echo "deb [arch=amd64 signed-by=/usr/share/keyrings/google-linux.gpg] http://dl.google.com/linux/chrome/deb/ stable main" > /etc/apt/sources.list.d/google-chrome.list && \
      apt-get update && \
      apt-get install -y --no-install-recommends google-chrome-stable && \
      ln -sf /usr/bin/google-chrome-stable /usr/local/bin/browser-bin; \
    else \
      apt-get install -y --no-install-recommends chromium && \
      ln -sf /usr/bin/chromium /usr/local/bin/browser-bin; \
    fi && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --uid 10001 nodeagent && \
    mkdir -p /tmp/chrome /var/log/browser-node && \
    chown -R nodeagent:nodeagent /tmp/chrome /var/log/browser-node /home/nodeagent

COPY --from=builder /out/node-agent /usr/local/bin/node-agent
COPY docker/browser-node/entrypoint.sh /usr/local/bin/browser-node-entrypoint
RUN chmod +x /usr/local/bin/browser-node-entrypoint

USER nodeagent
WORKDIR /home/nodeagent

ENV NODE_AGENT_HTTP_ADDR=:8091
ENV NODE_AGENT_HEARTBEAT_INTERVAL=5s
ENV NODE_AGENT_REQUEST_TIMEOUT=5s
ENV DISPLAY=:99
ENV CHROME_DEBUG_PORT=9222

EXPOSE 8091 9222

ENTRYPOINT ["/usr/local/bin/browser-node-entrypoint"]
