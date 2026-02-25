FROM golang:1.23.6-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod ./
COPY *.go ./
COPY templates/ templates/
COPY static/ static/
COPY data/ data/

ARG COMMIT_HASH
ARG BUILD_TIME
ARG BUILD_LOG_URL

RUN FINAL_HASH="${COMMIT_HASH}"; \
    if [ -z "$FINAL_HASH" ]; then \
      FINAL_HASH=$(git clone --depth=1 https://github.com/uSwapExchange/zero.git /tmp/repo 2>/dev/null && cd /tmp/repo && git rev-parse HEAD || echo "unknown"); \
      rm -rf /tmp/repo; \
    fi && \
    FINAL_TIME="${BUILD_TIME}"; \
    if [ -z "$FINAL_TIME" ]; then \
      FINAL_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
    fi && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X main.commitHash=${FINAL_HASH} -X main.buildTime=${FINAL_TIME} -X main.buildLogURL=${BUILD_LOG_URL}" \
      -o /zero

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /zero /zero
EXPOSE 3000
ENTRYPOINT ["/zero"]
