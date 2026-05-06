# 🚀 Sistema Distribuído de Brokers

Sistema distribuído com brokers, sensores e drones para monitoramento e resposta a eventos críticos.

## 📁 Estrutura do Projeto

```
sistema-distribuido-brokers/
├── cmd/
│   ├── broker/          # Aplicação principal do broker
│   ├── sensor/          # Aplicação de sensores
│   └── drone/           # Aplicação de drones
├── internal/            # Lógica interna (broker, eleição, mutex, fila, gossip)
├── pkg/                 # Tipos e utilitários compartilhados
├── docker-compose.yml   # Orquestração Docker (4 brokers, 8 drones, 12 sensores)
├── deploy_distribuido.sh # Script de deploy universal (qualquer rede)
├── deploy_ladica.sh      # Script específico para Lab LADICA
├── dashboard.html       # Interface web para monitoramento
├── dashboard_server.go  # Servidor de logs em tempo real
├── Dockerfile           # Imagem do broker
├── Dockerfile.drone     # Imagem do drone
├── Dockerfile.sensor    # Imagem do sensor
├── entrypoint.sh        # Script de entrada do broker
├── ips_maquinas_larsid.txt  # IPs do laboratório LARSID
├── go.mod               # Módulo Go
├── .gitignore           # Arquivos ignorados pelo Git
└── README.md            # Este arquivo
```

## 📊 Dashboard Web (Interface de Monitoramento)

Interface visual para acompanhar logs de brokers, drones e sensores em tempo real.

### 🚀 Como usar o Dashboard:

```bash
# 1. Inicie o sistema
docker-compose up -d

# 2. Inicie o servidor do dashboard
go run dashboard_server.go

# 3. Abra o navegador em http://localhost:8080
```

### ✨ Funcionalidades:
- 📡 **Logs em tempo real** de todos os 4 brokers
- 🎨 **Cores organizadas**: cada broker tem sua cor (roxo, rosa, azul, verde)
- 🔍 **Filtros**: por nível (INFO, ALERTA, AVISO, DEBUG) ou agente
- 🔎 **Busca** por texto nos logs
- 📊 **Estatísticas**: brokers, drones, sensores e alertas ativos
- ⚡ **Atualização automática** via Server-Sent Events

### 🌐 Acesso:
- URL: `http://localhost:8080`
- Requer: sistema rodando (`docker-compose up`)

---

## 🛠️ Requisitos

### Para Deploy Local:
- Go 1.21+
- Docker e Docker Compose

### Para Deploy Distribuído (múltiplas máquinas):
- Docker instalado em cada máquina
- SSH habilitado em cada máquina
- Acesso sem senha ou SSH configurado
- Conectividade de rede entre as máquinas
- Dependências: `ssh`, `sshpass`, `ping`

```bash
# Instalar dependências (Ubuntu/Debian)
sudo apt update
sudo apt install -y openssh-client sshpass iputils-ping
```

## 🚀 Execução no Laboratório

### 🌐 Opção 0: Deploy Automático Universal (Recomendado)

Script `deploy_distribuido.sh` funciona em **qualquer rede** (casa, laboratório, etc):

```bash
# Tornar executável
chmod +x deploy_distribuido.sh

# Executar (detecta rede automaticamente)
./deploy_distribuido.sh
```

**O que o script faz:**
1. 🔍 Detecta rede local automaticamente (192.168.x.x, 172.16.x.x, etc)
2. 📡 Escaneia máquinas com SSH disponível
3. 🎯 Permite selecionar 4 máquinas
4. 🐳 Deploy Docker em cada máquina automaticamente
5. ⚙️ Configura `LAB_IPS` automaticamente

**Configurações personalizadas:**
```bash
# Para casa com VMs Ubuntu
SSH_USER=ubuntu SSH_PASS=1234 SSH_PORT=22 ./deploy_distribuido.sh

# Para laboratório com porta SSH diferente
SSH_USER=tec502 SSH_PORT=2201 ./deploy_distribuido.sh
```

### 🎓 Opção LADICA: Laboratório LADICA (UFPR)

Script específico para o laboratório **LADICA** usando hostnames `ladica01` a `ladica15`:

```bash
# Tornar executável
chmod +x deploy_ladica.sh

# Executar (detecta e seleciona 4 máquinas automaticamente)
./deploy_ladica.sh
```

**Pré-requisitos:**
- Estar conectado na rede do laboratório LADICA
- Ter acesso SSH às máquinas (senha padrão: `ecomp`)

**Comandos personalizados:**
```bash
# Usar usuário/senha específicos
SSH_USER=aluno SSH_PASS=ecomp ./deploy_ladica.sh

# Ver ajuda
./deploy_ladica.sh --help
```

**O que o script faz:**
1. 🔍 Escaneia ladica01 a ladica15 (15 máquinas)
2. ✅ Seleciona automaticamente os **4 primeiros disponíveis**
3. 📊 Mostra todos os dados de conexão (hostname, IP, portas)
4. 🐳 Faz deploy Docker nas 4 máquinas
5. 📡 Configura `LAB_IPS` automaticamente entre elas

**Após o deploy:**
```bash
# Ver logs do Broker 1 (ladica01)
ssh ladica01 'docker logs -f broker'

# Status de todos os brokers
for h in ladica01 ladica02 ladica03 ladica04; do
    echo "=== $h ==="
    ssh $h 'docker ps | grep broker'
done

# Parar todos os brokers
for h in ladica01 ladica02 ladica03 ladica04; do
    ssh $h 'docker rm -f broker drone-01 drone-02 sensor-mov sensor-press sensor-temp'
done
```

**Arquivo:** `deploy_ladica.sh`

### 📍 Opção 1: Mesma Máquina (Mesma Rede)

#### Usando Docker (Recomendado)
```bash
# Iniciar sistema completo
docker-compose up -d

# Ver logs
docker-compose logs -f broker-1
docker-compose logs -f broker-2

# Parar
docker-compose down
```

#### Sem Docker (Binários nativos)
```bash
# Compilar
go build -o broker ./cmd/broker/main.go
go build -o drone ./cmd/drone/main.go
go build -o sensor ./cmd/sensor/main.go

# Executar (em terminais separados ou background)
./broker -id=broker-1 -porta-tcp=:9000 -porta-udp=:9001 -porta-ctrl=:9002 \
  -drones='{"drone-01":"localhost:9101","drone-02":"localhost:9102"}' \
  -peers='broker-2,localhost:9010,localhost:9011;broker-3,localhost:9020,localhost:9021;broker-4,localhost:9030,localhost:9031' &

./drone -id=drone-01 -port=:9101 &
./sensor -id=sensor-mov -tipo=movimento -local=setor-1 -brokers=localhost:9002 &
```

### 🌐 Opção 2: Máquinas Diferentes (IPs diferentes)

#### Preparação (em cada máquina)
```bash
# Clonar repositório
git clone <repo-url>
cd sistema-distribuido-brokers

# Compilar
go build -o broker ./cmd/broker/main.go
go build -o drone ./cmd/drone/main.go
go build -o sensor ./cmd/sensor/main.go
```

#### Máquina 1 (IP: 192.168.1.101) - Broker 1 + Sensores
```bash
./broker -id=broker-1 -porta-tcp=:9000 -porta-udp=:9001 -porta-ctrl=:9002 \
  -drones='{"drone-01":"192.168.1.101:9101","drone-02":"192.168.1.101:9102"}' \
  -peers='broker-2,192.168.1.102:9010,192.168.1.102:9011;broker-3,192.168.1.103:9020,192.168.1.103:9021;broker-4,192.168.1.104:9030,192.168.1.104:9031'

./drone -id=drone-01 -port=:9101 &
./drone -id=drone-02 -port=:9102 &

# 3 Sensores (movimento, pressão, temperatura)
./sensor -id=sensor-s1-mov -tipo=movimento -local=setor-norte-1 -brokers=192.168.1.101:9002 &
./sensor -id=sensor-s1-press -tipo=pressao -local=setor-norte-2 -brokers=192.168.1.101:9002 &
./sensor -id=sensor-s1-temp -tipo=temperatura -local=setor-norte-3 -brokers=192.168.1.101:9002 &
```

#### Máquina 2 (IP: 192.168.1.102) - Broker 2 + Sensores
```bash
./broker -id=broker-2 -porta-tcp=:9010 -porta-udp=:9011 -porta-ctrl=:9012 \
  -drones='{"drone-03":"192.168.1.102:9103","drone-04":"192.168.1.102:9104"}' \
  -peers='broker-1,192.168.1.101:9000,192.168.1.101:9001;broker-3,192.168.1.103:9020,192.168.1.103:9021;broker-4,192.168.1.104:9030,192.168.1.104:9031'

./drone -id=drone-03 -port=:9103 &
./drone -id=drone-04 -port=:9104 &

# 3 Sensores (movimento, pressão, temperatura)
./sensor -id=sensor-s2-mov -tipo=movimento -local=setor-sul-1 -brokers=192.168.1.102:9012 &
./sensor -id=sensor-s2-press -tipo=pressao -local=setor-sul-2 -brokers=192.168.1.102:9012 &
./sensor -id=sensor-s2-temp -tipo=temperatura -local=setor-sul-3 -brokers=192.168.1.102:9012 &
```

#### Máquina 3 (IP: 192.168.1.103) - Broker 3
```bash
./broker -id=broker-3 -porta-tcp=:9020 -porta-udp=:9021 -porta-ctrl=:9022 \
  -drones='{"drone-05":"192.168.1.103:9105","drone-06":"192.168.1.103:9106"}' \
  -peers='broker-1,192.168.1.101:9000,192.168.1.101:9001;broker-2,192.168.1.102:9010,192.168.1.102:9011;broker-4,192.168.1.104:9030,192.168.1.104:9031'

./drone -id=drone-05 -port=:9105 &
./drone -id=drone-06 -port=:9106 &
```

#### Máquina 4 (IP: 192.168.1.104) - Broker 4 + Sensores
```bash
./broker -id=broker-4 -porta-tcp=:9030 -porta-udp=:9031 -porta-ctrl=:9032 \
  -drones='{"drone-07":"192.168.1.104:9107","drone-08":"192.168.1.104:9108"}' \
  -peers='broker-1,192.168.1.101:9000,192.168.1.101:9001;broker-2,192.168.1.102:9010,192.168.1.102:9011;broker-3,192.168.1.103:9020,192.168.1.103:9021'

./drone -id=drone-07 -port=:9107 &
./drone -id=drone-08 -port=:9108 &

# 3 Sensores (movimento, pressão, temperatura)
./sensor -id=sensor-s4-mov -tipo=movimento -local=setor-oeste-1 -brokers=192.168.1.104:9032 &
./sensor -id=sensor-s4-press -tipo=pressao -local=setor-oeste-2 -brokers=192.168.1.104:9032 &
./sensor -id=sensor-s4-temp -tipo=temperatura -local=setor-oeste-3 -brokers=192.168.1.104:9032 &
```

### 🛑 Parar Sistema

```bash
# Matar todos os processos
pkill -f broker && pkill -f drone && pkill -f sensor

# Ou com Docker
docker-compose down
```

---

## 📋 Resumo de Portas

| Componente | TCP | UDP | Sensores | Qtd Sensores |
|-------------|-----|-----|----------|--------------|
| Broker 1 | 9000 | 9001 | 9002 | 3 (mov, press, temp) |
| Broker 2 | 9010 | 9011 | 9012 | 3 (mov, press, temp) |
| Broker 3 | 9020 | 9021 | 9022 | 3 (mov, press, temp) |
| Broker 4 | 9030 | 9031 | 9032 | 3 (mov, press, temp) |
| Drone 01-08 | 9101-9108 | - | - | - |

## 🔧 Parâmetros de Configuração

### Broker
- `-id`: ID único do broker
- `-porta-tcp`: Porta TCP para conexões de clientes
- `-porta-udp`: Porta UDP para heartbeats
- `-porta-ctrl`: Porta de controle
- `-drones`: Configuração JSON dos drones
- `-peers`: Lista de peers (ID,TCP,UDP;...)

### Sensor
- `-id`: ID do sensor
- `-tipo`: Tipo do sensor (movimento, temperatura, pressao)
- `-local`: Localização do sensor
- `-brokers`: Lista de brokers separados por vírgula

### Drone
- `-id`: ID do drone
- `-port`: Porta TCP do drone

## 🐛 Verificação e Debug

### Verificar processos ativos
```bash
ps aux | grep -E "(broker|sensor|drone)"
```

### Verificar portas em uso
```bash
netstat -tulpn | grep -E "(9000|9001|9002|9003|9004|9005|9101|9102)"
```

### Testar conexão com brokers
```bash
telnet localhost 9000
telnet localhost 9010
```

## � Comandos Úteis (Deploy Distribuído)

### 📡 Verificar Status dos Brokers
```bash
# Status de todos os brokers (substitua IPs pelos reais)
for ip in 192.168.1.101 192.168.1.102 192.168.1.103 192.168.1.104; do
    echo "=== $ip ==="
    ssh tec502@$ip 'docker ps | grep broker'
done
```

### 📋 Ver Logs dos Brokers
```bash
# Logs de um broker específico
ssh tec502@192.168.1.101 'docker logs -f broker'

# Logs de todos os brokers (4 terminais)
ssh tec502@192.168.1.101 'docker logs -f broker' &
ssh tec502@192.168.1.102 'docker logs -f broker' &
ssh tec502@192.168.1.103 'docker logs -f broker' &
ssh tec502@192.168.1.104 'docker logs -f broker' &
wait
```

### 🛑 Parar Sistema Distribuído
```bash
# Parar todos os brokers
for ip in 192.168.1.101 192.168.1.102 192.168.1.103 192.168.1.104; do
    ssh tec502@$ip 'docker rm -f broker drone-01 drone-02 sensor-mov sensor-press sensor-temp' 2>/dev/null || true
done
```

### 🔄 Reiniciar um Broker Específico
```bash
# Reiniciar Broker 2
ssh tec502@192.168.1.102 'docker restart broker'

# Ver logs após reinício
ssh tec502@192.168.1.102 'docker logs -f broker'
```

### 📊 Monitorar Recursos
```bash
# Uso de memória/CPU em todas as máquinas
for ip in 192.168.1.101 192.168.1.102 192.168.1.103 192.168.1.104; do
    echo "=== $ip ==="
    ssh tec502@$ip 'docker stats --no-stream'
done
```

## 📋 Ordem Recomendada de Inicialização

1. **Deploy Automático**: `./deploy_distribuido.sh` (recomendado)
2. **Ou Manual**: Configure cada máquina individualmente
3. Espere 10 segundos entre cada broker
4. Monitore os logs
5. Verifique comunicação entre peers

## 🌟 Funcionalidades

### ✅ Implementadas
- **Eleição distribuída**: Algoritmo Bully para escolha de líder
- **Heartbeats**: Detecção automática de falhas
- **Protocolo Gossip**: Propagação de estado
- **Sensores**: Geração de dados telemétricos
- **Drones**: Execução de missões
- **Fila distribuída**: Processamento de requisições
- **Mutex distribuído**: Exclusão mútua

### 📡 Tipos de Sensores
- **Movimento**: Detecta objetos (0-1)
- **Temperatura**: Monitoramento térmico (-20°C a 50°C)
- **Pressão**: Condições climáticas (950-1050 hPa)

### 🚁 Funcionalidades dos Drones
- Recebimento de comandos de missão
- Execução com tempo variável (5-15 segundos)
- Notificação de disponibilidade

## 🔮 Eventos Críticos Detectados

- **OBJETO_NAO_IDENTIFICADO**: Movimento suspeito
- **CONDICAO_CLIMATICA_SEVERA**: Variação anormal de pressão
- **TEMPERATURA_EXTREMA**: Temperaturas fora do normal

## 📝 Logs e Monitoramento

O sistema gera logs detalhados mostrando:
- Inicialização dos componentes
- Processo de eleição
- Detecção de eventos críticos
- Status dos drones e sensores
- Recuperação de falhas

---

**Sistema completo pronto para uso em ambientes distribuídos!** 🚀
