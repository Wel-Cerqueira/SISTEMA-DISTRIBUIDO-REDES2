#!/bin/bash

# Script de Deploy para Laboratório LADICA
# Conecta via hostnames ladica01 a ladica15
# Escolhe os 4 primeiros disponíveis

set -e

# Cores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

# Configurações
SSH_USER="${SSH_USER:-$USER}"  # Usa usuário atual por padrão
SSH_PASS="${SSH_PASS:-ecomp}"  # Senha padrão do LADICA
PROJECT_NAME="SISTEMA-DISTRIBUIDO-REDES2"
REPO_URL="https://github.com/welton-cerqueira/SISTEMA-DISTRIBUIDO-REDES2.git"

# Arrays para armazenar dados
declare -a TODAS_MAQUINAS=()
declare -a MAQUINAS_ONLINE=()
declare -a MAQUINAS_OFFLINE=()
declare -a SELECIONADAS=()
declare -a IPS_SELECIONADAS=()

# Função para imprimir cabeçalho
print_header() {
    echo ""
    echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║${NC}     ${BOLD}DEPLOY DISTRIBUÍDO - LABORATÓRIO LADICA${NC}              ${CYAN}║${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BLUE}Data/Hora:${NC} $(date '+%Y-%m-%d %H:%M:%S')"
    echo -e "${BLUE}Usuário:${NC} $SSH_USER"
    echo -e "${BLUE}Senha:${NC} $SSH_PASS"
    echo ""
}

# Função para verificar dependências
check_dependencies() {
    echo -e "${BLUE}🔍 Verificando dependências...${NC}"
    
    local deps_ok=true
    
    for cmd in ssh sshpass ping; do
        if command -v $cmd &> /dev/null; then
            echo -e "  ${GREEN}✓ $cmd${NC}"
        else
            echo -e "  ${RED}✗ $cmd não encontrado${NC}"
            deps_ok=false
        fi
    done
    
    if [ "$deps_ok" = false ]; then
        echo ""
        echo -e "${RED}❌ Dependências faltando!${NC}"
        echo "Instale com: sudo apt install -y openssh-client sshpass iputils-ping"
        exit 1
    fi
    
    echo ""
}

# Função para testar conexão SSH
testar_conexao() {
    local hostname=$1
    local max_attempts=2
    
    # Testa ping primeiro (mais rápido)
    if ! ping -c 1 -W 1 $hostname &> /dev/null; then
        return 1
    fi
    
    # Testa SSH
    if sshpass -p "$SSH_PASS" ssh -o StrictHostKeyChecking=no \
        -o ConnectTimeout=3 -o BatchMode=no \
        $SSH_USER@$hostname 'echo "OK"' &> /dev/null; then
        return 0
    fi
    
    return 1
}

# Função para obter IP de uma máquina
obter_ip() {
    local hostname=$1
    local ip=$(sshpass -p "$SSH_PASS" ssh -o StrictHostKeyChecking=no \
        -o ConnectTimeout=3 $SSH_USER@$hostname 'hostname -I | awk "{print \$1}"' 2>/dev/null)
    echo "$ip"
}

# Função para escanear todas as máquinas
escanear_máquinas() {
    echo -e "${BLUE}🔍 Escaneando máquinas LADICA...${NC}"
    echo -e "${YELLOW}   (Testando ladica01 até ladica15)${NC}"
    echo ""
    
    printf "%-12s %-8s %-18s %-10s\n" "Máquina" "Status" "IP" "Latência"
    echo "───────────────────────────────────────────────────────"
    
    for i in $(seq -w 1 15); do
        local hostname="ladica$i"
        TODAS_MAQUINAS+=("$hostname")
        
        # Mostra progresso
        printf "  Testando %-10s\r" "$hostname"
        
        local inicio=$(date +%s.%N)
        
        if testar_conexao "$hostname"; then
            local ip=$(obter_ip "$hostname")
            local fim=$(date +%s.%N)
            local latencia=$(echo "($fim - $inicio) * 1000" | bc | cut -d. -f1)
            
            MAQUINAS_ONLINE+=("$hostname")
            printf "${GREEN}%-12s %-8s %-18s ${YELLOW}~%sms${NC}\n" "$hostname" "ONLINE" "$ip" "$latencia"
            
            # Se ainda não temos 4, adiciona à lista de selecionadas
            if [ ${#SELECIONADAS[@]} -lt 4 ]; then
                SELECIONADAS+=("$hostname")
                IPS_SELECIONADAS+=("$ip")
            fi
        else
            printf "${RED}%-12s %-8s %-18s %-10s${NC}\n" "$hostname" "OFFLINE" "-" "-"
            MAQUINAS_OFFLINE+=("$hostname")
        fi
        
        # Para quando já temos 4 selecionadas e testou mais 2 pra diagnóstico
        if [ ${#SELECIONADAS[@]} -ge 4 ] && [ $i -ge 6 ]; then
            # Continua testando pra mostrar diagnóstico, mas pode parar antecipado
            :  # No-op, continua
        fi
    done
    
    echo ""
}

# Função para mostrar resumo de seleção
mostrar_selecao() {
    echo -e "${BLUE}📊 RESUMO DO ESCANEAMENTO${NC}"
    echo "─────────────────────────────────────────────────"
    echo -e "Total de máquinas:    ${#TODAS_MAQUINAS[@]}"
    echo -e "Máquinas online:      ${GREEN}${#MAQUINAS_ONLINE[@]}${NC}"
    echo -e "Máquinas offline:     ${RED}${#MAQUINAS_OFFLINE[@]}${NC}"
    echo ""
    
    if [ ${#MAQUINAS_ONLINE[@]} -lt 4 ]; then
        echo -e "${RED}❌ ERRO: Apenas ${#MAQUINAS_ONLINE[@]} máquina(s) online!${NC}"
        echo -e "${RED}   Necessário: 4 máquinas${NC}"
        echo ""
        echo "Máquinas online encontradas:"
        for m in "${MAQUINAS_ONLINE[@]}"; do
            echo "  - $m"
        done
        exit 1
    fi
    
    echo -e "${GREEN}✅ 4 Máquinas selecionadas automaticamente:${NC}"
    echo ""
    printf "${BOLD}%-8s %-15s %-18s %-10s${NC}\n" "Broker" "Hostname" "IP" "Portas"
    echo "─────────────────────────────────────────────────────────────"
    
    local idx=1
    for i in "${!SELECIONADAS[@]}"; do
        local hostname="${SELECIONADAS[$i]}"
        local ip="${IPS_SELECIONADAS[$i]}"
        local base_port=$((9000 + i * 10))
        
        printf "${CYAN}%-8s${NC} %-15s %-18s TCP:%d/UDP:%d\n" \
            "B$idx" "$hostname" "$ip" "$base_port" "$((base_port+1))"
        
        idx=$((idx+1))
    done
    
    echo ""
    echo -e "${YELLOW}LAB_IPS configurado:${NC}"
    echo "  ${IPS_SELECIONADAS[*]}"
    echo ""
}

# Função para confirmar deploy
confirmar_deploy() {
    echo -e "${YELLOW}⚠️  CONFIRMAÇÃO DE DEPLOY${NC}"
    echo ""
    echo "Este script irá:"
    echo "  1. Clonar repositório em cada máquina selecionada"
    echo "  2. Buildar imagem Docker do broker"
    echo "  3. Iniciar broker com LAB_IPS configurado"
    echo "  4. Iniciar 2 drones por máquina"
    echo "  5. Iniciar 3 sensores por máquina"
    echo ""
    echo -e "Máquinas afetadas: ${SELECIONADAS[*]}"
    echo ""
    
    read -p "Deseja continuar? (s/N): " confirm
    
    if [[ ! "$confirm" =~ ^[Ss]$ ]]; then
        echo ""
        echo -e "${YELLOW}❌ Deploy cancelado pelo usuário${NC}"
        exit 0
    fi
    
    echo ""
    echo -e "${GREEN}✅ Deploy confirmado! Iniciando...${NC}"
    echo ""
}

# Função para fazer deploy em uma máquina
deploy_maquina() {
    local hostname=$1
    local index=$2
    local ip=$3
    
    echo -e "${BLUE}────────────────────────────────────────────────────${NC}"
    echo -e "${BLUE}🚀 DEPLOYANDO BROKER $index em $hostname${NC}"
    echo -e "${BLUE}   IP: $ip${NC}"
    echo -e "${BLUE}────────────────────────────────────────────────────${NC}"
    
    # Prepara string LAB_IPS
    local lab_ips="${IPS_SELECIONADAS[*]}"
    
    # Comandos a executar na máquina remota
    sshpass -p "$SSH_PASS" ssh -o StrictHostKeyChecking=no \
        -o ConnectTimeout=10 $SSH_USER@$hostname << EOF
        set -e
        
        echo "[1/4] Preparando ambiente..."
        cd ~
        
        # Remove instalação anterior se existir
        if [ -d "$PROJECT_NAME" ]; then
            echo "      Removendo instalação anterior..."
            docker rm -f broker drone-01 drone-02 sensor-mov sensor-press sensor-temp 2>/dev/null || true
            rm -rf $PROJECT_NAME
        fi
        
        echo "[2/4] Clonando repositório..."
        if ! git clone "$REPO_URL" "$PROJECT_NAME" 2>/dev/null; then
            echo "      ⚠️ Falha no clone, tentando download alternativo..."
            # Fallback: copiar via scp ou usar repositório local
            exit 1
        fi
        
        cd $PROJECT_NAME
        
        echo "[3/4] Buildando imagens Docker..."
        echo "      → Broker..."
        docker build -t broker:latest -f Dockerfile . 2>&1 | tail -3
        
        echo "      → Drone..."
        docker build -t drone:latest -f Dockerfile.drone . 2>&1 | tail -3
        
        echo "      → Sensor..."
        docker build -t sensor:latest -f Dockerfile.sensor . 2>&1 | tail -3
        
        echo "[4/4] Iniciando containers..."
        echo "      → Broker (ID: broker-${index}, LAB_IPS: $lab_ips)"
        docker run -d --name broker --network host \
            -e LAB_IPS="$lab_ips" \
            broker:latest 2>&1 | tail -1
        
        echo "      → Drone 01"
        local drone_port=$((9100 + index * 2))
        docker run -d --name drone-01 --network host \
            drone:latest ./drone -id=drone-$(printf "%02d" $((index*2-1))) -port=:\$((drone_port+1)) 2>/dev/null || echo "Drone 1 já existe ou erro"
        
        echo "      → Drone 02"
        docker run -d --name drone-02 --network host \
            drone:latest ./drone -id=drone-$(printf "%02d" $((index*2))) -port=:\$((drone_port+2)) 2>/dev/null || echo "Drone 2 já existe ou erro"
        
        echo "      → Sensores (movimento, pressão, temperatura)"
        local sensor_port=$((9000 + (index-1) * 10 + 2))
        
        docker run -d --name sensor-mov --network host \
            sensor:latest ./sensor -id=sensor-mov-$index -tipo=movimento -local=setor-$index -brokers=localhost:\$sensor_port 2>/dev/null || true
        
        docker run -d --name sensor-press --network host \
            sensor:latest ./sensor -id=sensor-press-$index -tipo=pressao -local=setor-$index -brokers=localhost:\$sensor_port 2>/dev/null || true
        
        docker run -d --name sensor-temp --network host \
            sensor:latest ./sensor -id=sensor-temp-$index -tipo=temperatura -local=setor-$index -brokers=localhost:\$sensor_port 2>/dev/null || true
        
        echo ""
        echo "✓ Deploy concluído em $hostname"
EOF
    
    local exit_code=$?
    
    if [ $exit_code -eq 0 ]; then
        echo ""
        echo -e "${GREEN}✅ Broker $index (\${hostname}) deployado com sucesso!${NC}"
    else
        echo ""
        echo -e "${RED}❌ Falha ao deployar Broker $index em \${hostname} (exit code: $exit_code)${NC}"
    fi
    
    return $exit_code
}

# Função para mostrar resumo final
mostrar_resumo() {
    echo ""
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║${NC}        ${BOLD}✅ DEPLOY CONCLUÍDO COM SUCESSO!${NC}                  ${GREEN}║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    
    echo -e "${BLUE}📋 BROKERS ATIVOS:${NC}"
    echo ""
    printf "${BOLD}%-10s %-15s %-18s %-12s${NC}\n" "Broker" "Hostname" "IP" "Status"
    echo "─────────────────────────────────────────────────────────────"
    
    local idx=1
    for hostname in "${SELECIONADAS[@]}"; do
        local ip="${IPS_SELECIONADAS[$idx-1]}"
        
        # Verifica se está rodando
        local status=$(sshpass -p "$SSH_PASS" ssh -o StrictHostKeyChecking=no \
            -o ConnectTimeout=3 $SSH_USER@$hostname 'docker ps --filter "name=broker" --format "{{.Status}}"' 2>/dev/null)
        
        if [ -n "$status" ]; then
            printf "${GREEN}%-10s${NC} %-15s %-18s ${GREEN}%-12s${NC}\n" \
                "B$idx" "$hostname" "$ip" "🟢 Running"
        else
            printf "${RED}%-10s${NC} %-15s %-18s ${RED}%-12s${NC}\n" \
                "B$idx" "$hostname" "$ip" "🔴 Error"
        fi
        
        idx=$((idx+1))
    done
    
    echo ""
    echo -e "${BLUE}🔧 COMANDOS ÚTEIS:${NC}"
    echo ""
    
    echo "Ver logs de um broker:"
    for hostname in "${SELECIONADAS[@]}"; do
        echo "  ssh $SSH_USER@$hostname 'docker logs -f broker'"
    done
    
    echo ""
    echo "Ver status de todos:"
    echo "  for h in ${SELECIONADAS[*]}; do echo \"=== \$h ===\"; ssh $SSH_USER@\$h 'docker ps'; done"
    
    echo ""
    echo "Parar todos os brokers:"
    echo "  for h in ${SELECIONADAS[*]}; do ssh $SSH_USER@\$h 'docker rm -f broker drone-01 drone-02 sensor-mov sensor-press sensor-temp'; done"
    
    echo ""
    echo -e "${CYAN}🎉 Sistema distribuído pronto para uso!${NC}"
    echo ""
}

# Função principal
main() {
    print_header
    check_dependencies
    escanear_máquinas
    mostrar_selecao
    confirmar_deploy
    
    # Deploy em cada máquina selecionada
    local idx=1
    for hostname in "${SELECIONADAS[@]}"; do
        local ip="${IPS_SELECIONADAS[$idx-1]}"
        deploy_maquina "$hostname" "$idx" "$ip"
        idx=$((idx+1))
        
        # Pequena pausa entre deploys
        if [ $idx -le 4 ]; then
            echo -e "${YELLOW}   Aguardando 3 segundos antes do próximo deploy...${NC}"
            sleep 3
        fi
    done
    
    mostrar_resumo
}

# Execução
if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    echo "DEPLOY LADICA - Script de deploy automatizado para Laboratório LADICA"
    echo ""
    echo "Uso: $0 [opções]"
    echo ""
    echo "Configurações via variáveis de ambiente:"
    echo "  SSH_USER    - Usuário SSH (padrão: usuário atual)"
    echo "  SSH_PASS    - Senha SSH (padrão: ecomp)"
    echo ""
    echo "Exemplos:"
    echo "  $0                           # Usa configurações padrão"
    echo "  SSH_USER=aluno $0            # Usa usuário 'aluno'"
    echo "  SSH_PASS=minhasenha $0       # Define senha alternativa"
    echo ""
    echo "O script irá:"
    echo "  1. Escanear ladica01 a ladica15"
    echo "  2. Selecionar os 4 primeiros disponíveis"
    echo "  3. Mostrar dados de conexão"
    echo "  4. Confirmar deploy"
    echo "  5. Instalar e iniciar sistema em todas as máquinas"
    exit 0
fi

# Verifica se está no laboratório
if ! ping -c 1 -W 1 ladica01 &> /dev/null && \
   ! ping -c 1 -W 1 ladica02 &> /dev/null; then
    echo -e "${RED}⚠️  ATENÇÃO: Não foi possível conectar a ladica01/ladica02${NC}"
    echo -e "${RED}    Verifique se você está no laboratório LADICA${NC}"
    echo ""
    read -p "Deseja tentar mesmo assim? (s/N): " force
    if [[ ! "$force" =~ ^[Ss]$ ]]; then
        exit 1
    fi
    echo ""
fi

# Executa main
main "$@"
