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
	"time"
)

// LogEntry representa uma entrada de log
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Agent     string `json:"agent"`
	Message   string `json:"message"`
}

var (
	// Regex para parsear logs do docker-compose
	logRegex = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d+)\]\s*\[(\w+)\]\s*(.*)$`)
	
	// Mapeamento de containers para brokers
	brokerMap = map[string]string{
		"broker-1":   "broker-1",
		"broker-2":   "broker-2",
		"broker-3":   "broker-3",
		"broker-4":   "broker-4",
	}
)

func main() {
	// Serve o dashboard HTML
	http.HandleFunc("/", serveDashboard)
	
	// Endpoint SSE para logs
	http.HandleFunc("/logs", handleLogs)
	
	// API para status dos containers
	http.HandleFunc("/api/status", handleStatus)
	
	port := "8080"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}
	
	fmt.Printf("🚀 Dashboard server iniciado em http://localhost:%s\n", port)
	fmt.Println("📊 Abra o navegador e acesse o link acima")
	fmt.Println("🛑 Pressione Ctrl+C para parar")
	
	log.Fatal(http.ListenAndServe(":"+port, nil))
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
	// Configura headers para SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming não suportado", http.StatusInternalServerError)
		return
	}
	
	// Inicia docker-compose logs
	cmd := exec.Command("docker-compose", "logs", "-f", "--tail", "100",
		"broker-1", "broker-2", "broker-3", "broker-4")
	
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
	
	// Lê logs linha por linha
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		
		// Parseia a linha de log
		entry := parseLogLine(line)
		if entry != nil {
			// Envia como evento SSE
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func parseLogLine(line string) *LogEntry {
	// Remove prefixo do container (ex: "broker-1  | ")
	parts := strings.SplitN(line, "|", 2)
	if len(parts) < 2 {
		return nil
	}
	
	container := strings.TrimSpace(parts[0])
	message := strings.TrimSpace(parts[1])
	
	// Determina qual broker
	agent := "broker-1"
	for key := range brokerMap {
		if strings.Contains(container, key) {
			agent = key
			break
		}
	}
	
	// Extrai timestamp, nível e mensagem
	matches := logRegex.FindStringSubmatch(message)
	if len(matches) >= 4 {
		return &LogEntry{
			Timestamp: matches[1],
			Level:     matches[2],
			Agent:     agent,
			Message:   matches[3],
		}
	}
	
	// Se não conseguiu parsear, retorna como mensagem genérica
	return &LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05.000"),
		Level:     "INFO",
		Agent:     agent,
		Message:   message,
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	
	// Executa docker-compose ps
	cmd := exec.Command("docker-compose", "ps", "--format", "json")
	output, err := cmd.Output()
	
	if err != nil {
		http.Error(w, `{"error": "Falha ao obter status"}`, http.StatusInternalServerError)
		return
	}
	
	w.Write(output)
}
