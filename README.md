# 🚀 Sistema Distribuído de Brokers

Sistema distribuído com brokers, sensores e drones para monitoramento e resposta a eventos críticos.

## 📁 Estrutura do Projeto

```
sistema-distribuido-brokers/
├── cmd/
│   ├── broker/          # Aplicação principal do broker
│   ├── sensor/          # Aplicação de sensores
│   └── drone/           # Aplicação de drones
├── internal/
│   ├── broker/          # Lógica do broker
│   ├── eleicao/         # Algoritmo de eleição
│   ├── exclusao_mutua/  # Mutex distribuído
│   ├── fila/           # Fila distribuída
│   └── gossip/         # Protocolo gossip
├── pkg/
│   ├── tipos/          # Tipos de dados compartilhados
│   └── utils/          # Utilitários
├── Dockerfile           # Build do broker
├── docker-compose.yml   # Orquestração completa
└── README.md           # Este arquivo
```

## 🛠️ Requisitos

- Go 1.21+
- Docker e Docker Compose (opcional)

## 🚀 Iniciando o Sistema

### Opção 1: Sem Docker (Recomendado para desenvolvimento)

#### 1. Compilar Todos os Componentes
```bash
# Compilar broker
go build -o broker ./cmd/broker/main.go

# Compilar sensor  
go build -o sensor ./cmd/sensor/main.go

# Compilar drone
go build -o drone ./cmd/drone/main.go
```

#### 2. Iniciar Sistema Completo (4 terminais)

**Terminal 1 - Broker 1 (Líder)**
```bash
./broker -id=broker-001 -porta-tcp=:9000 -porta-udp=:9001 -porta-ctrl=:9100 -drones='{"drone-01":"localhost:9101","drone-02":"localhost:9102"}' -peers=broker-002,localhost:9002,localhost:9003,broker-003,localhost:9004,localhost:9005
```

**Terminal 2 - Broker 2**
```bash
./broker -id=broker-002 -porta-tcp=:9002 -porta-udp=:9003 -porta-ctrl=:9102 -drones='{"drone-01":"localhost:9101","drone-02":"localhost:9102"}' -peers=broker-001,localhost:9000,localhost:9001,broker-003,localhost:9004,localhost:9005
```

**Terminal 3 - Broker 3**
```bash
./broker -id=broker-003 -porta-tcp=:9004 -porta-udp=:9005 -porta-ctrl=:9104 -drones='{"drone-01":"localhost:9101","drone-02":"localhost:9102"}' -peers=broker-001,localhost:9000,localhost:9001,broker-002,localhost:9002,localhost:9003
```

**Terminal 4 - Drones**
```bash
# Drone 1
./drone -id=drone-01 -port=:9101 &
# Drone 2  
./drone -id=drone-02 -port=:9102 &
```

**Terminal 5 - Sensores**
```bash
# Sensor de movimento
./sensor -id=sensor-movimento-01 -tipo=movimento -local=setor-norte -brokers=localhost:9000,localhost:9002,localhost:9004 &

# Sensor de temperatura
./sensor -id=sensor-temp-01 -tipo=temperatura -local=setor-sul -brokers=localhost:9000,localhost:9002,localhost:9004 &

# Sensor de pressão
./sensor -id=sensor-pressao-01 -tipo=pressao -local=setor-leste -brokers=localhost:9000,localhost:9002,localhost:9004 &
```

#### 3. Versão Simplificada (2 brokers + 1 drone + 1 sensor)

**Terminal 1 - Broker 1**
```bash
./broker -id=broker-001 -porta-tcp=:9000 -porta-udp=:9001 -porta-ctrl=:9100 -drones='{"drone-01":"localhost:9101"}' -peers=broker-002,localhost:9002,localhost:9003
```

**Terminal 2 - Broker 2**
```bash
./broker -id=broker-002 -porta-tcp=:9002 -porta-udp=:9003 -porta-ctrl=:9102 -drones='{"drone-01":"localhost:9101"}' -peers=broker-001,localhost:9000,localhost:9001
```

**Terminal 3 - Drone**
```bash
./drone -id=drone-01 -port=:9101
```

**Terminal 4 - Sensor**
```bash
./sensor -id=sensor-movimento-01 -tipo=movimento -local=setor-norte -brokers=localhost:9000,localhost:9002
```

### Opção 2: Com Docker

#### Iniciar todo o sistema
```bash
docker-compose up -d
```

#### Ver logs
```bash
docker-compose logs -f
```

#### Parar sistema
```bash
docker-compose down
```

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
telnet localhost 9002
```

## 🛑 Parar Sistema

### Parar todos os processos
```bash
pkill -f "broker"
pkill -f "sensor" 
pkill -f "drone"
```

### Ou com Ctrl+C em cada terminal

## 📋 Ordem Recomendada de Inicialização

1. Compile todos os componentes
2. Inicie os brokers (espere 10 segundos entre cada)
3. Inicie os drones
4. Inicie os sensores
5. Monitore os logs

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
