package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sistema-distribuido-brokers/internal/broker"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"syscall"
)

func main() {
	// Parâmetros de linha de comando
	id := flag.String("id", os.Getenv("BROKER_ID"), "ID do broker")                          // Identificador único do broker (ex: "broker-1")
	portaTCP := flag.String("porta-tcp", os.Getenv("PORT_TCP"), "Porta TCP")                 // Porta para conexões de clientes (ex: ":9000")
	portaUDP := flag.String("porta-udp", os.Getenv("PORT_UDP"), "Porta UDP")                 // Porta para heartbeats entre brokers (ex: ":9001")
	portaCTRL := flag.String("porta-ctrl", os.Getenv("PORT_SENSORES"), "Porta dos Sensores") // Porta para receber dados dos sensores (ex: ":9002")
	dronesConfig := flag.String("drones", "", "Configuração dos drones (JSON)")              //  Configuração JSON dos drones disponíveis
	peers := flag.String("peers", os.Getenv("PEERS"), "Lista de peers (ID,TCP,UDP;...)")     // Lista dos outros brokers no formato "ID,TCP,UDP;ID,TCP,UDP;..."
	flag.Parse()

	// Validação
	if *id == "" || *portaTCP == "" || *portaUDP == "" || *portaCTRL == "" {
		fmt.Println("Erro: Parâmetros obrigatórios não fornecidos")
		fmt.Println("Uso: ./broker -id=ID -porta-tcp=:9000 -porta-udp=:9001 -porta-ctrl=:9002 -peers=...")
		os.Exit(1)
	}

	// Processa lista de peers (formato: "ID,TCP,UDP;ID,TCP,UDP;...")
	var listaVizinhos []string
	if *peers != "" {
		listaVizinhos = parsePeers(*peers)
		for i := 0; i < len(listaVizinhos); i += 3 {
			if i+2 < len(listaVizinhos) {
				peer := fmt.Sprintf("%s,%s,%s", listaVizinhos[i], listaVizinhos[i+1], listaVizinhos[i+2])
				utils.RegistrarLog("INFO", "Peer configurado: %s", peer)
			}
		}
	}

	// Cria broker
	broker, err := broker.NovoBroker(*id, *portaTCP, *portaUDP, *portaCTRL, listaVizinhos, *dronesConfig)
	if err != nil {
		utils.RegistrarLog("ERRO", "Falha ao criar broker: %v", err)
		os.Exit(1)
	}

	// Inicia broker
	if err := broker.Iniciar(); err != nil {
		utils.RegistrarLog("ERRO", "Falha ao iniciar broker: %v", err)
		os.Exit(1)
	}

	// Aguarda sinal de término
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	utils.RegistrarLog("INFO", "Encerrando broker...")
	broker.Parar()
}

// parsePeers converte string de peers no formato "ID,TCP,UDP;ID,TCP,UDP;..."
// em uma lista plana [ID1, TCP1, UDP1, ID2, TCP2, UDP2, ...]
func parsePeers(peersStr string) []string {
	var resultado []string
	if peersStr == "" {
		return resultado
	}

	// Primeiro divide por ';' para separar cada peer
	peerEntries := strings.Split(peersStr, ";")
	for _, entry := range peerEntries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// Depois divide por ',' para obter ID, TCP, UDP
		parts := strings.Split(entry, ",")
		for _, part := range parts {
			resultado = append(resultado, strings.TrimSpace(part))
		}
	}
	return resultado
}
