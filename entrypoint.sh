#!/bin/bash
set -e

if [ -z "$LAB_IPS" ]; then
    echo "ERRO: Variável LAB_IPS não definida."
    exit 1
fi

# Lista de IPs da LAB_IPS (separadores: espaço, vírgula, ponto e vírgula)
LAB_IPS_LIST=($(echo "$LAB_IPS" | tr ',;' ' ' | xargs -n1 | sort -u))

# Obtém o IP local principal (primeiro que não seja loopback)
MY_IP=$(ip -4 addr show scope global | grep inet | awk '{print $2}' | cut -d/ -f1 | head -1)
if [ -z "$MY_IP" ]; then
    echo "ERRO: Não foi possível detectar IP local."
    exit 1
fi

echo "IP local detectado: $MY_IP"

# Gera ID baseado no último octeto
octeto=$(echo "$MY_IP" | cut -d. -f4)
BROKER_ID="broker-${octeto}"
base=$((9000 + (octeto - 1) * 10))
TCP_PORT=":${base}"
UDP_PORT=":$(($base+1))"
CTRL_PORT=":$(($base+2))"

echo "Configuração: ID=$BROKER_ID TCP=$TCP_PORT UDP=$UDP_PORT CTRL=$CTRL_PORT"

# Constrói peers a partir da LAB_IPS, excluindo o próprio IP (se presente)
PEERS_ARRAY=()
for ip in "${LAB_IPS_LIST[@]}"; do
    # Se o IP da lista for igual ao meu IP, pula
    if [ "$ip" = "$MY_IP" ]; then
        continue
    fi
    oct=$(echo "$ip" | cut -d. -f4)
    base_peer=$((9000 + (oct - 1) * 10))
    tcp="${ip}:${base_peer}"
    udp="${ip}:$(($base_peer+1))"
    id="broker-${oct}"
    PEERS_ARRAY+=("$id" "$tcp" "$udp")
done

PEERS_STRING=$(IFS=,; echo "${PEERS_ARRAY[*]}")
echo "Peers: $PEERS_STRING"

exec /root/broker \
    -id="$BROKER_ID" \
    -porta-tcp="$TCP_PORT" \
    -porta-udp="$UDP_PORT" \
    -porta-ctrl="$CTRL_PORT" \
    -peers="$PEERS_STRING"