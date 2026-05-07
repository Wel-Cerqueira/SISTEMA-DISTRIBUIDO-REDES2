package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Agent     string `json:"agent"`
	Message   string `json:"message"`
}

type Device struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "drone" ou "sensor"
}

type BrokerInfo struct {
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	Sensors  []Device  `json:"sensors"`
	Drones   []Device  `json:"drones"`
	LastSeen time.Time `json:"lastSeen"`
}

var (
	logRegex = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d+)\]\s*\[(\w+)\]\s*(.*)$`)

	brokerMap = map[string]string{
		"broker-1": "broker-1",
		"broker-2": "broker-2",
		"broker-3": "broker-3",
		"broker-4": "broker-4",
	}

	// Estado compartilhado
	brokersState = make(map[string]*BrokerInfo)
	stateMutex   sync.RWMutex
)

func main() {
	// Inicializa estado dos brokers
	initBrokersState()

	// Inicia goroutine para atualizar IPs periodicamente (caso mudem)
	go updateBrokersIPsPeriodically()

	http.HandleFunc("/", serveDashboard)
	http.HandleFunc("/logs", handleLogs)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/brokers", handleBrokersInfo) // novo endpoint

	port := "8080"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}

	fmt.Printf("🚀 Dashboard server iniciado em http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func initBrokersState() {
	for name := range brokerMap {
		brokersState[name] = &BrokerInfo{
			Name:     name,
			Sensors:  []Device{},
			Drones:   []Device{},
			LastSeen: time.Now(),
		}
	}
	updateBrokersIPs() // preenche IPs iniciais
}

func updateBrokersIPs() {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	for name := range brokersState {
		ip := getContainerIP(name)
		if ip != "" {
			brokersState[name].IP = ip
		}
	}
}

func updateBrokersIPsPeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	for range ticker.C {
		updateBrokersIPs()
	}
}

func getContainerIP(containerName string) string {
	cmd := exec.Command("docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(output))
	return ip
}

func handleBrokersInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	stateMutex.RLock()
	defer stateMutex.RUnlock()

	// Copia para evitar mutação durante marshaling
	response := make([]BrokerInfo, 0, len(brokersState))
	for _, info := range brokersState {
		response = append(response, *info)
	}

	json.NewEncoder(w).Encode(response)
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	content, err := os.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "Dashboard não encontrado", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming não suportado", http.StatusInternalServerError)
		return
	}

	cmd := exec.Command("docker", "compose", "logs", "-f", "--tail", "10")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Erro ao criar pipe: %v", err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Erro ao iniciar docker-compose logs: %v", err)
		return
	}

	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		entry := parseLogLine(line)
		if entry != nil {
			// Atualiza associações de sensores/drones baseado na mensagem
			updateDevicesFromLog(entry)

			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// Mapeamento de containers para brokers (sensores e drones -> broker)
// Baseado na configuração do docker-compose.yml
var sensorToBrokerMap = map[string]string{
	"sensor-s1": "broker-1",
	"sensor-s2": "broker-2",
	"sensor-s3": "broker-3",
	"sensor-s4": "broker-4",
}

var droneToBrokerMap = map[string]string{
	"drone-01": "broker-1",
	"drone-02": "broker-2",
	"drone-03": "broker-3",
	"drone-04": "broker-4",
	"drone-05": "broker-1",
	"drone-06": "broker-2",
	"drone-07": "broker-3",
	"drone-08": "broker-4",
}

func parseLogLine(line string) *LogEntry {
	parts := strings.SplitN(line, "|", 2)
	if len(parts) < 2 {
		return nil
	}

	container := strings.TrimSpace(parts[0])
	message := strings.TrimSpace(parts[1])

	// Detecta o agente baseado no nome do container
	agent := "broker-1"
	deviceID := ""

	// Verifica se é um broker
	for key := range brokerMap {
		if strings.Contains(container, key) {
			agent = key
			break
		}
	}

	// Verifica se é um sensor (sensor-s1-temp, sensor-s2-mov, etc.)
	for sensorPrefix, broker := range sensorToBrokerMap {
		if strings.Contains(container, sensorPrefix) {
			agent = broker
			// Extrai o ID completo do sensor (ex: sensor-s1-temp)
			reSensorID := regexp.MustCompile(`(sensor-[a-zA-Z0-9-]+)`)
			if matches := reSensorID.FindStringSubmatch(container); len(matches) >= 2 {
				deviceID = matches[1]
			}
			// Adiciona o sensor automaticamente se estiver enviando telemetria
			if deviceID != "" && strings.Contains(message, "telemetria") {
				addDevice(agent, deviceID, "sensor")
			}
			break
		}
	}

	// Verifica se é um drone (drone-01, drone-02, etc.)
	for droneIDKey, broker := range droneToBrokerMap {
		if strings.Contains(container, droneIDKey) {
			agent = broker
			deviceID = droneIDKey
			// Adiciona o drone automaticamente se estiver enviando status
			if strings.Contains(message, "DISPONIVEL") || strings.Contains(message, "EM_MISSAO") {
				addDevice(agent, deviceID, "drone")
			}
			break
		}
	}

	matches := logRegex.FindStringSubmatch(message)
	if len(matches) >= 4 {
		return &LogEntry{
			Timestamp: matches[1],
			Level:     matches[2],
			Agent:     agent,
			Message:   matches[3],
		}
	}

	// Tenta detectar nível do log baseado em palavras-chave
	level := "INFO"
	msgUpper := strings.ToUpper(message)
	if strings.Contains(msgUpper, "ALERTA") || strings.Contains(msgUpper, "CRÍTICO") || strings.Contains(msgUpper, "CRITICO") {
		level = "ALERTA"
	} else if strings.Contains(msgUpper, "AVISO") || strings.Contains(msgUpper, "WARNING") {
		level = "AVISO"
	} else if strings.Contains(msgUpper, "ERRO") || strings.Contains(msgUpper, "ERROR") {
		level = "ALERTA"
	} else if strings.Contains(msgUpper, "DEBUG") {
		level = "DEBUG"
	}

	return &LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05.000"),
		Level:     level,
		Agent:     agent,
		Message:   message,
	}
}

// updateDevicesFromLog analisa a mensagem e adiciona/remove sensores/drones
// Baseado em padrões comuns, ajuste conforme os logs reais do sistema
func updateDevicesFromLog(entry *LogEntry) {
	msg := entry.Message
	broker := entry.Agent

	// Padrões baseados nos logs reais do sistema
	// Sensor iniciado: "Sensor sensor-s1-temp (temperatura) em setor-norte-3 iniciado"
	// Sensor finalizado: "Sensor sensor-s1-temp finalizado"
	// Drone disponível: "drone-01 DISPONIVEL" ou "Drone drone-01 iniciado"
	// Drone em missão: "drone-01 req=xxx setor=yyy estado=EM_MISSAO"

	reSensorIniciado := regexp.MustCompile(`[Ss]ensor\s+(sensor-[a-zA-Z0-9-]+)`)
	reSensorFinalizado := regexp.MustCompile(`[Ss]ensor\s+(sensor-[a-zA-Z0-9-]+)\s+finalizado`)
	reDroneIniciado := regexp.MustCompile(`(?:[Dd]rone[-\s]*)([a-zA-Z0-9-]+).*?(?:DISPONIVEL|iniciado|conectado)`)
	reDroneFinalizado := regexp.MustCompile(`(?:[Dd]rone[-\s]*)([a-zA-Z0-9-]+).*?(?:finalizado|desconectado|exited)`)

	switch {
	case reSensorIniciado.MatchString(msg):
		matches := reSensorIniciado.FindStringSubmatch(msg)
		if len(matches) >= 2 {
			addDevice(broker, matches[1], "sensor")
		}
	case reSensorFinalizado.MatchString(msg):
		matches := reSensorFinalizado.FindStringSubmatch(msg)
		if len(matches) >= 2 {
			removeDevice(broker, matches[1], "sensor")
		}
	case reDroneIniciado.MatchString(msg):
		matches := reDroneIniciado.FindStringSubmatch(msg)
		if len(matches) >= 2 {
			addDevice(broker, matches[1], "drone")
		}
	case reDroneFinalizado.MatchString(msg):
		matches := reDroneFinalizado.FindStringSubmatch(msg)
		if len(matches) >= 2 {
			removeDevice(broker, matches[1], "drone")
		}
	}
}

func addDevice(brokerName, deviceID, deviceType string) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	broker, exists := brokersState[brokerName]
	if !exists {
		return
	}

	device := Device{ID: deviceID, Type: deviceType}

	// Evita duplicatas
	if deviceType == "drone" {
		for _, d := range broker.Drones {
			if d.ID == deviceID {
				return
			}
		}
		broker.Drones = append(broker.Drones, device)
	} else if deviceType == "sensor" {
		for _, d := range broker.Sensors {
			if d.ID == deviceID {
				return
			}
		}
		broker.Sensors = append(broker.Sensors, device)
	}
	broker.LastSeen = time.Now()
}

func removeDevice(brokerName, deviceID, deviceType string) {
	stateMutex.Lock()
	defer stateMutex.Unlock()

	broker, exists := brokersState[brokerName]
	if !exists {
		return
	}

	if deviceType == "drone" {
		newList := []Device{}
		for _, d := range broker.Drones {
			if d.ID != deviceID {
				newList = append(newList, d)
			}
		}
		broker.Drones = newList
	} else if deviceType == "sensor" {
		newList := []Device{}
		for _, d := range broker.Sensors {
			if d.ID != deviceID {
				newList = append(newList, d)
			}
		}
		broker.Sensors = newList
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	cmd := exec.Command("docker", "compose", "ps", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, `{"error": "Falha ao obter status"}`, http.StatusInternalServerError)
		return
	}
	w.Write(output)
}
