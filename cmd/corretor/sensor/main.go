package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"strings"
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

var (
	// Lista de brokers para failover
	brokersList     []string
	currentBroker   int
	brokerConection net.Conn
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
	for {
		if err := conectarBroker(sensor); err != nil {
			fmt.Printf("Erro ao conectar: %v. Tentando próximo broker...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	// Loop principal de envio de dados
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dados := gerarDadosSensor(sensor)
			if err := enviarDados(dados); err != nil {
				fmt.Printf("Erro ao enviar dados: %v\n", err)
				// Tenta reconectar
				reconectar(sensor)
			}
		}
	}
}

// conectarBroker conecta ao broker atual
func conectarBroker(sensor *Sensor) error {
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
		brokerConection = conn
		return nil
	}

	return fmt.Errorf("resposta inesperada do broker")
}

// reconectar tenta reconectar a um broker disponível
func reconectar(sensor *Sensor) {
	if brokerConection != nil {
		brokerConection.Close()
		brokerConection = nil
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
	fmt.Println("Todos os brokers estão indisponíveis. Tentando novamente em 10 segundos...")
	time.Sleep(10 * time.Second)
	reconectar(sensor)
}

// gerarDadosSensor gera dados aleatórios baseado no tipo do sensor
func gerarDadosSensor(sensor *Sensor) *SensorData {
	var valor float64

	switch sensor.Tipo {
	case "temperatura":
		valor = 20 + rand.Float64()*15 // 20-35°C
	case "pressao":
		valor = 1013 + (rand.Float64()-0.5)*50 // 988-1038 hPa
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

// enviarDados envia dados para o broker
func enviarDados(dados *SensorData) error {
	if brokerConection == nil {
		return fmt.Errorf("sem conexão com broker")
	}

	encoder := json.NewEncoder(brokerConection)
	if err := encoder.Encode(dados); err != nil {
		return err
	}

	fmt.Printf("Dados enviados: %s = %.2f\n", dados.Tipo, dados.Valor)
	return nil
}
