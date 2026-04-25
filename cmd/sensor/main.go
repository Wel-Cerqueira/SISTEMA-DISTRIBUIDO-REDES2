package main

import (
	"flag"
	"os"
	"os/signal"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"syscall"
	"time"
)

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

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			utils.RegistrarLog("INFO", "Sensor %s em operacao", *id)
		case <-sigCh:
			utils.RegistrarLog("INFO", "Sensor %s finalizado", *id)
			return
		}
	}
}
