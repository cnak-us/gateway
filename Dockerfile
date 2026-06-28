FROM golang:1.26.2-alpine AS builder

ENV GOFIPS140=v1.0.0

WORKDIR /workspace

COPY go.mod go.sum ./
COPY pkg/ ./pkg/

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o gateway .

FROM alpine:3.22
RUN apk upgrade --no-cache openssl libcrypto3 libssl3 ca-certificates
RUN apk --no-cache add ca-certificates
ENV GODEBUG=fips140=on
WORKDIR /app
COPY --from=builder /workspace/gateway .
EXPOSE 8088 8089 8443 8090

RUN addgroup -S app && adduser -S app -G app
RUN mkdir -p /app/data && chown app:app /app/data
USER app

HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://localhost:8090/health || exit 1

ENTRYPOINT ["./gateway"]
