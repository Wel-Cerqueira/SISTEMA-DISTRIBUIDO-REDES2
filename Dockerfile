FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copia arquivos de dependências
COPY go.mod ./
RUN go mod download || true

# Copia código fonte
COPY . .

# Compila a aplicação
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o broker ./cmd/broker

# Imagem final
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copia o binário compilado
COPY --from=builder /app/broker .

# Expõe portas
EXPOSE 9000 9001

# Comando para executar
CMD ["./broker"]