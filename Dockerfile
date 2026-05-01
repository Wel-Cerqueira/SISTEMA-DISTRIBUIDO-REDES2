# Estágio de build (já existente, mantido)
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o broker ./cmd/broker

# Imagem final
FROM alpine:latest
RUN apk --no-cache add ca-certificates iproute2 bash
WORKDIR /root/
COPY --from=builder /app/broker .
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expõe portas (opcional, apenas documentação)
EXPOSE 9000-9032/tcp 9001-9031/udp

ENTRYPOINT ["/entrypoint.sh"]