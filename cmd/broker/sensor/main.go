package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Estruturas para comunicação
type Sensor struct {
	ID          string `json:"id"`
	Tipo        string `json:"tipo"`
	Localizacao string `json:"localizacao"`
}

type SensorData struct {
	ID           string    `json:"id"`
	Tipo         string    `json:"tipo"`
	Valor        float64   `json:"valor"`
	Localizacao  string    `json:"localizacao"`
	CarimboTempo time.Time `json:"carimbo_tempo"`
	SetorID      string    `json:"setor_id"`
}

// RespostaACK representa a confirmação do broker
type RespostaACK struct {
	Status        string    `json:"status"`
	Timestamp     time.Time `json:"timestamp"`
	BrokerID      string    `json:"broker_id"`
	DadosID       string    `json:"dados_id"`
	EventoCritico bool      `json:"evento_critico"`
}

var (
	// Lista de brokers para failover
	brokersList   []string
	currentBroker int
	brokerConexao net.Conn
	executando    bool = true
)

func main() {
	// Flags de linha de comando
	sensorID := flag.String("id", fmt.Sprintf("sensor-%d", rand.Intn(10000)), "ID do sensor")
	sensorTipo := flag.String("tipo", "movimento", "Tipo do sensor (movimento, temperatura, pressao)")
	localizacao := flag.String("local", "setor-alfa", "Localizacao do sensor")
	brokers := flag.String("brokers", "localhost:9002,localhost:9012,localhost:9022", "Lista de brokers (csv)")
	flag.Parse()

	// Configura a lista de brokers
	brokersList = strings.Split(*brokers, ",")
	currentBroker = 0

	rand.Seed(time.Now().UnixNano())

	sensor := &Sensor{
		ID:          *sensorID,
		Tipo:        *sensorTipo,
		Localizacao: *localizacao,
	}

	fmt.Printf("Iniciando sensor %s (%s) em %s\n", sensor.ID, sensor.Tipo, sensor.Localizacao)
	fmt.Printf("Brokers disponíveis: %v\n", brokersList)

	// Conecta ao broker
	if err := conectarBroker(sensor); err != nil {
		fmt.Printf("Erro ao conectar inicial: %v\n", err)
		os.Exit(1)
	}

	// Configura tratamento de sinal para desligamento gracioso
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Loop principal de envio de dados
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	fmt.Println("Sensor iniciado. Enviando dados... (Pressione Ctrl+C para parar)")

	for executando {
		select {
		case <-ticker.C:
			dados := gerarDadosSensor(sensor)
			if err := enviarDados(dados); err != nil {
				fmt.Printf("Erro ao enviar dados: %v\n", err)
				// Tenta reconectar
				reconectar(sensor)
			}
		case <-sigCh:
			fmt.Println("\nRecebido sinal de parada. Encerrando sensor...")
			executando = false
			if brokerConexao != nil {
				brokerConexao.Close()
			}
			fmt.Println("Sensor encerrado.")
			return
		}
	}
}

// conectarBroker conecta ao broker atual
func conectarBroker(sensor *Sensor) error {
	if currentBroker >= len(brokersList) {
		currentBroker = 0
	}
	endereco := brokersList[currentBroker]
	fmt.Printf("Tentando conectar ao broker %s...\n", endereco)

	conn, err := net.DialTimeout("tcp", endereco, 10*time.Second)
	if err != nil {
		return fmt.Errorf("falha ao conectar ao broker %s: %v", endereco, err)
	}

	// Registra o sensor
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(sensor); err != nil {
		conn.Close()
		return fmt.Errorf("falha no registro: %v", err)
	}

	// Aguarda confirmação
	var resposta map[string]string
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resposta); err != nil {
		conn.Close()
		return fmt.Errorf("falha ao receber confirmação: %v", err)
	}

	if status, ok := resposta["status"]; ok && status == "registrado" {
		fmt.Printf("Sensor registrado com sucesso no broker %s (setor %s)\n",
			endereco, resposta["setor_id"])
		brokerConexao = conn
		return nil
	}

	conn.Close()
	return fmt.Errorf("resposta inesperada do broker")
}

// reconectar tenta reconectar a um broker disponível
func reconectar(sensor *Sensor) {
	if brokerConexao != nil {
		brokerConexao.Close()
		brokerConexao = nil
	}

	// Tenta o próximo broker na lista
	for i := 0; i < len(brokersList); i++ {
		currentBroker = (currentBroker + 1) % len(brokersList)
		fmt.Printf("Tentando broker alternativo: %s\n", brokersList[currentBroker])

		if err := conectarBroker(sensor); err == nil {
			fmt.Println("Reconectado com sucesso!")
			return
		}
		time.Sleep(2 * time.Second)
	}

	// Se todos falharam, espera e tenta novamente
	fmt.Println("Todos os brokers estão indisponíveis. Tentando novamente em 5 segundos...")
	time.Sleep(5 * time.Second)
	reconectar(sensor)
}

// gerarDadosSensor gera dados aleatórios baseado no tipo do sensor
func gerarDadosSensor(sensor *Sensor) *SensorData {
	var valor float64

	switch strings.ToLower(sensor.Tipo) {
	case "temperatura":
		valor = -20 + rand.Float64()*70 // -20 a 50°C
	case "pressao":
		valor = 950 + rand.Float64()*100 // 950-1050 hPa
	case "movimento":
		valor = rand.Float64() // 0-1, probabilidade de detecção
	default:
		valor = rand.Float64() * 100
	}

	return &SensorData{
		ID:           sensor.ID,
		Tipo:         sensor.Tipo,
		Valor:        valor,
		Localizacao:  sensor.Localizacao,
		CarimboTempo: time.Now(),
		SetorID:      "", // Será preenchido pelo broker
	}
}

// enviarDados envia dados para o broker e aguarda ACK
func enviarDados(dados *SensorData) error {
	if brokerConexao == nil {
		return fmt.Errorf("sem conexão com broker")
	}

	// Define timeout para a operação
	brokerConexao.SetWriteDeadline(time.Now().Add(5 * time.Second))

	encoder := json.NewEncoder(brokerConexao)
	if err := encoder.Encode(dados); err != nil {
		return fmt.Errorf("falha ao enviar: %v", err)
	}

	// Aguarda ACK do broker (timeout de 3 segundos)
	brokerConexao.SetReadDeadline(time.Now().Add(3 * time.Second))

	var ack RespostaACK
	decoder := json.NewDecoder(brokerConexao)
	if err := decoder.Decode(&ack); err != nil {
		return fmt.Errorf("falha ao receber ACK: %v", err)
	}

	// Reseta os deadlines
	brokerConexao.SetReadDeadline(time.Time{})
	brokerConexao.SetWriteDeadline(time.Time{})

	if ack.Status != "recebido" {
		return fmt.Errorf("ACK inválido: status=%s", ack.Status)
	}

	if ack.EventoCritico {
		fmt.Printf("⚠️  ALERTA: Evento crítico detectado pelo broker! (gravidade: %s)\n", dados.Tipo)
	} else {
		fmt.Printf("✓ Dados enviados: %s = %.2f (ACK de %s)\n", dados.Tipo, dados.Valor, ack.BrokerID)
	}

	return nil
}
