package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"syscall"
	"time"
)

type SensorData struct {
	ID           string    `json:"id"`
	Tipo         string    `json:"tipo"`
	Valor        float64   `json:"valor"`
	Localizacao  string    `json:"localizacao"`
	CarimboTempo time.Time `json:"carimbo_tempo"`
	SetorID      string    `json:"setor_id"`
}

func main() {
	idDefault := os.Getenv("SENSOR_ID")
	if idDefault == "" {
		idDefault = "sensor-generico"
	}
	tipoDefault := os.Getenv("SENSOR_TIPO")
	if tipoDefault == "" {
		tipoDefault = "movimento"
	}
	localDefault := os.Getenv("LOCALIZACAO")
	if localDefault == "" {
		localDefault = "desconhecida"
	}
	brokersDefault := os.Getenv("BROKERS")

	id := flag.String("id", idDefault, "ID do sensor")
	tipo := flag.String("tipo", tipoDefault, "tipo do sensor")
	local := flag.String("local", localDefault, "localizacao do sensor")
	brokers := flag.String("brokers", brokersDefault, "lista de brokers separados por virgula")
	flag.Parse()

	utils.RegistrarLog("INFO", "Sensor %s (%s) em %s iniciado", *id, *tipo, *local)
	if strings.TrimSpace(*brokers) != "" {
		utils.RegistrarLog("INFO", "Brokers configurados: %s", *brokers)
	}

	brokerList := strings.Split(*brokers, ",")
	if len(brokerList) == 0 {
		utils.RegistrarLog("ERRO", "Nenhum broker configurado")
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			// Gera dados telemétricos aleatórios autônomos
			dados := gerarDadosTelemetricos(*id, *tipo, *local, rng)

			// Envia para múltiplos brokers (descentralizado)
			for _, broker := range brokerList {
				go func(b string) {
					if err := enviarDadosTelemetricos(strings.TrimSpace(b), dados); err != nil {
						utils.RegistrarLog("ERRO", "Falha ao enviar dados para %s: %v", b, err)
					}
				}(broker)
			}

		case <-sigCh:
			utils.RegistrarLog("INFO", "Sensor %s finalizado", *id)
			return
		}
	}
}

func gerarDadosTelemetricos(id, tipo, local string, rng *rand.Rand) SensorData {
	var valor float64

	switch strings.ToLower(tipo) {
	case "movimento":
		// Gera valores de movimento de 0 a 1
		valor = rng.Float64()
	case "temperatura":
		// Gera temperatura entre -20 e 50 graus
		valor = -20 + rng.Float64()*70
	case "pressao":
		// Gera pressão entre 950 e 1050 hPa
		valor = 950 + rng.Float64()*100
	default:
		valor = rng.Float64()
	}

	return SensorData{
		ID:           id,
		Tipo:         tipo,
		Valor:        valor,
		Localizacao:  local,
		CarimboTempo: time.Now(),
		SetorID:      extrairSetorDaLocalizacao(local),
	}
}

func extrairSetorDaLocalizacao(local string) string {
	// Extrai setor da localização (ex: "setor-norte" -> "norte")
	parts := strings.Split(local, "-")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "desconhecido"
}

// No sensor - aguardar confirmação
func enviarDadosTelemetricos(broker string, dados SensorData) error {
	conn, err := net.DialTimeout("tcp", broker, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	dadosJSON, _ := json.Marshal(dados)
	conn.Write(append(dadosJSON, '\n'))

	// Aguarda ACK do broker
	var ack map[string]interface{}
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&ack); err != nil {
		return err
	}

	if ack["status"] != "recebido" {
		return fmt.Errorf("ack invalido: %v", ack)
	}
	return nil
}
