# syntax=docker/dockerfile:1
# tellurion-risk · 风控控制面服务（ADR-036 G0；同步裁决仍留 core）
FROM golang:1.25-alpine AS builder

WORKDIR /src
RUN apk add --no-cache git

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath \
    -ldflags "-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o /out/risk-service ./cmd/riskservice

FROM alpine:3.19

WORKDIR /app
RUN apk add --no-cache ca-certificates netcat-openbsd tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    rm -rf /var/cache/apk/*

COPY --from=builder /out/risk-service /app/risk-service

EXPOSE 8089
# 本仓今天没有公开 /healthz；G0 用 TCP 探针，公开健康端点归 G1。
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD nc -z 127.0.0.1 8089 || exit 1

USER nobody
ENTRYPOINT ["/app/risk-service"]