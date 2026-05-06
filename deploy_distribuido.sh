#!/bin/bash

# Script de Deploy Distribuído Universal
# Funciona em qualquer rede: casa, laboratório LARSID, ou qualquer outra
# Detecta máquinas disponíveis via SSH e faz deploy de 4 brokers com Docker

set -e

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configurações padrão
REPO_URL="https://github.com/welton-cerqueira/SISTEMA-DISTRIBUIDO-REDES2.git"
PROJECT_DIR="SISTEMA-DISTRIBUIDO-REDES2"
SSH_USER="${SSH_USER:-tec502}"
SSH_PASS="${SSH_PASS:-larsid}"
SSH_PORT="${SSH_PORT:-22}"
PING_TIMEOUT=2
SSH_TIMEOUT=5

# Detectar rede local automaticamente
detectar_rede() {
    echo -e "${BLUE}🔍 Detectando rede local...${NC}"
    
    # Pega IP local e máscara
    LOCAL_IP=$(ip -4 addr show scope global | grep inet | awk '{print $2}' | cut -d/ -f1 | head -1)
    if [ -z "$LOCAL_IP" ]; then
        echo -e "${RED}❌ Não foi possível detectar IP local${NC}"
        exit 1
    fi
    
    # Extrai prefixo da rede (ex: 192.168.1)
    NETWORK_PREFIX=$(echo "$LOCAL_IP" | cut -d. -f1-3)
    echo -e "${GREEN}✓ Rede detectada: $NETWORK_PREFIX.0/24${NC}"
    echo -e "${GREEN}✓ IP local: $LOCAL_IP${NC}"
}

# Verificar dependências
check_deps() {
    local missing=()
    for cmd in ssh sshpass ping; do
        if ! command -v $cmd &> /dev/null; then
            missing+=($cmd)
        fi
    done
    
    if [ ${#missing[@]} -gt 0 ]; then
        echo -e "${RED}❌ Dependências faltando: ${missing[*]}${NC}"
        echo "Instale com: sudo apt install ssh sshpass iputils-ping"
        exit 1
    fi
}

# Testar conexão SSH em uma máquina
testar_ssh() {
    local ip=$1
    local port=$2
    
    # Primeiro testa ping
    if ! ping -c 1 -W $PING_TIMEOUT $ip > /dev/null 2>&1; then
        return 1
    fi
    
    # Depois testa SSH (sem senha, apenas verifica se porta responde)
    if timeout $SSH_TIMEOUT bash -c "echo >/dev/tcp/$ip/$port" 2>/dev/null; then
        return 0
    fi
    
    return 1
}

# Descobrir máquinas disponíveis na rede
descobrir_maquinas() {
    echo ""
    echo -e "${BLUE}🔍 Escaneando máquinas na rede $NETWORK_PREFIX.0/24...${NC}"
    echo "(Isso pode levar alguns segundos...)"
    echo ""
    
    MAQUINAS_DISPONIVEIS=()
    
    # Escaneia IPs .1 a .254 (limitado a range comum .1-.20 para agilidade)
    for i in $(seq 1 20); do
        ip="${NETWORK_PREFIX}.$i"
        
        # Pula o próprio IP
        if [ "$ip" = "$LOCAL_IP" ]; then
            continue
        fi
        
        printf "  Testando $ip...\r"
        
        if testar_ssh $ip $SSH_PORT; then
            echo -e "  ${GREEN}✓ $ip - SSH disponível${NC}"
            MAQUINAS_DISPONIVEIS+=("$ip")
        fi
    done
    
    echo ""
    echo -e "${GREEN}✓ Encontradas ${#MAQUINAS_DISPONIVEIS[@]} máquinas${NC}"
}

# Selecionar 4 máquinas
selecionar_maquinas() {
    local total=${#MAQUINAS_DISPONIVEIS[@]}
    
    if [ $total -lt 4 ]; then
        echo -e "${RED}❌ Apenas $total máquina(s) disponível(is). Necessário 4 máquinas.${NC}"
        echo ""
        echo "Opções:"
        echo "  1. Inicie mais VMs/máquinas na rede"
        echo "  2. Use docker-compose local (1 máquina)"
        echo "  3. Verifique se o SSH está habilitado nas máquinas"
        exit 1
    fi
    
    echo ""
    echo -e "${BLUE}📋 Máquinas disponíveis:${NC}"
    for i in "${!MAQUINAS_DISPONIVEIS[@]}"; do
        echo "  [$i] ${MAQUINAS_DISPONIVEIS[$i]}"
    done
    
    echo ""
    echo -e "${YELLOW}Selecione 4 máquinas (digite os números separados por espaço):${NC}"
    echo "Exemplo: 0 1 2 3"
    echo "Ou pressione ENTER para usar as primeiras 4 disponíveis"
    read -r selecao
    
    if [ -z "$selecao" ]; then
        # Usa as primeiras 4 automaticamente
        SELECIONADAS=("${MAQUINAS_DISPONIVEIS[@]:0:4}")
        echo -e "${GREEN}✓ Usando: ${SELECIONADAS[*]}${NC}"
    else
        # Valida seleção
        SELECIONADAS=()
        for num in $selecao; do
            if [[ "$num" =~ ^[0-9]+$ ]] && [ $num -ge 0 ] && [ $num -lt $total ]; then
                SELECIONADAS+=("${MAQUINAS_DISPONIVEIS[$num]}")
            else
                echo -e "${RED}❌ Seleção inválida: $num${NC}"
                exit 1
            fi
        done
        
        if [ ${#SELECIONADAS[@]} -ne 4 ]; then
            echo -e "${RED}❌ Você deve selecionar exatamente 4 máquinas (selecionou ${#SELECIONADAS[@]})${NC}"
            exit 1
        fi
    fi
    
    echo ""
    echo -e "${GREEN}✓ 4 Brokers serão deployados em:${NC}"
    for ip in "${SELECIONADAS[@]}"; do
        echo "    - $ip"
    done
}

# Preparar string LAB_IPS
preparar_ips() {
    LAB_IPS_STRING="${SELECIONADAS[*]}"
    echo ""
    echo -e "${BLUE}🔧 Configuração LAB_IPS:${NC} $LAB_IPS_STRING"
}

# Deploy do broker em uma máquina
deploy_broker() {
    local ip=$1
    local index=$2
    
    echo ""
    echo -e "${BLUE}🚀 Deployando Broker $index em $ip...${NC}"
    
    # Comandos a executar na máquina remota
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=$SSH_TIMEOUT \
        -p $SSH_PORT "$SSH_USER@$ip" << EOF
        set -e
        
        # Cleanup anterior
        docker rm -f broker 2>/dev/null || true
        rm -rf ~/$PROJECT_DIR
        
        # Clone
        cd ~
        if ! git clone "$REPO_URL" "$PROJECT_DIR" 2>/dev/null; then
            echo "Git clone falhou, tentando download manual..."
            exit 1
        fi
        
        cd $PROJECT_DIR
        
        # Build
        echo "Building broker..."
        docker build -t broker:latest -f Dockerfile . 2>&1 | tail -5
        
        # Run
        echo "Starting broker..."
        docker run -d \
            --name broker \
            --network host \
            -e LAB_IPS="$LAB_IPS_STRING" \
            broker:latest 2>&1
        
        echo "✓ Broker iniciado"
EOF
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Broker $index deployado com sucesso em $ip${NC}"
    else
        echo -e "${RED}❌ Falha ao deployar Broker $index em $ip${NC}"
        return 1
    fi
}

# Deploy de drones e sensores em uma máquina
deploy_drones_sensores() {
    local ip=$1
    local index=$2
    local base_port=$((9100 + index * 2))
    
    echo -e "${BLUE}🚁 Deployando drones e sensores em $ip...${NC}"
    
    ssh -o StrictHostKeyChecking=no -o ConnectTimeout=$SSH_TIMEOUT \
        -p $SSH_PORT "$SSH_USER@$ip" << EOF
        cd ~/$PROJECT_DIR
        
        # Build
        docker build -t drone:latest -f Dockerfile.drone . 2>&1 | tail -3
        docker build -t sensor:latest -f Dockerfile.sensor . 2>&1 | tail -3
        
        # Cleanup
        docker rm -f drone-01 drone-02 2>/dev/null || true
        docker rm -f sensor-mov sensor-press sensor-temp 2>/dev/null || true
        
        # Run drones (2 por máquina)
        docker run -d --name drone-01 --network host drone:latest ./drone -id=drone-$(printf "%02d" $((index*2+1))) -port=:$((base_port+1)) 2>/dev/null || echo "Drone 1 já existe"
        docker run -d --name drone-02 --network host drone:latest ./drone -id=drone-$(printf "%02d" $((index*2+2))) -port=:$((base_port+2)) 2>/dev/null || echo "Drone 2 já existe"
        
        echo "✓ Drones iniciados"
EOF
    
    echo -e "${GREEN}✓ Drones em $ip${NC}"
}

# Menu principal
menu() {
    echo ""
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}  DEPLOY DISTRIBUÍDO UNIVERSAL                 ${NC}"
    echo -e "${BLUE}  Funciona em qualquer rede (Casa/Lab/Outros)   ${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
    
    check_deps
    detectar_rede
    descobrir_maquinas
    selecionar_maquinas
    preparar_ips
    
    # Confirmação
    echo ""
    echo -e "${YELLOW}⚠️  Isso irá: ${NC}"
    echo "  - Clonar o repositório em 4 máquinas"
    echo "  - Buildar imagens Docker em cada uma"
    echo "  - Iniciar 4 brokers interconectados"
    echo "  - Iniciar 2 drones por máquina (8 total)"
    echo ""
    read -p "Continuar? (s/n): " confirm
    if [[ ! "$confirm" =~ ^[Ss]$ ]]; then
        echo "Cancelado."
        exit 0
    fi
    
    # Deploy em cada máquina
    local idx=0
    for ip in "${SELECIONADAS[@]}"; do
        deploy_broker "$ip" "$((idx+1))"
        deploy_drones_sensores "$ip" "$idx"
        idx=$((idx+1))
    done
    
    # Resumo
    echo ""
    echo -e "${GREEN}================================================${NC}"
    echo -e "${GREEN}  ✅ SISTEMA INICIADO COM SUCESSO!            ${NC}"
    echo -e "${GREEN}================================================${NC}"
    echo ""
    echo "📋 Brokers rodando em:"
    for ip in "${SELECIONADAS[@]}"; do
        echo "   - $ip (SSH: $SSH_USER@$ip:$SSH_PORT)"
    done
    echo ""
    echo "🔧 Comandos úteis:"
    echo "  Ver logs:     ssh $SSH_USER@${SELECIONADAS[0]} 'docker logs -f broker'"
    echo "  Parar tudo:   ssh $SSH_USER@${SELECIONADAS[0]} 'docker rm -f broker drone-01 drone-02'"
    echo "  Status:       for ip in ${SELECIONADAS[*]}; do ssh $SSH_USER@\$ip 'docker ps'; done"
    echo ""
}

# Execução
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    echo "Uso: $0"
    echo ""
    echo "Variáveis de ambiente:"
    echo "  SSH_USER    - Usuário SSH (padrão: tec502)"
    echo "  SSH_PASS    - Senha SSH (padrão: larsid)"
    echo "  SSH_PORT    - Porta SSH (padrão: 22)"
    echo ""
    echo "Exemplo:"
    echo "  SSH_USER=ubuntu SSH_PASS=1234 SSH_PORT=22 $0"
    exit 0
fi

menu
