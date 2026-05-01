package tipos

import (
	"fmt"
	"time"
)

// Mensagem representa uma mensagem trocada entre brokers
type Mensagem struct {
	Tipo            string      `json:"tipo"`
	OrigemID        string      `json:"origem_id"`
	DestinoID       string      `json:"destino_id"`
	Dados           interface{} `json:"dados"`
	CarimboTempo    time.Time   `json:"carimbo_tempo"`
	NumeroSequencia uint64      `json:"numero_sequencia"`
}

// EstadoBroker representa o estado de um broker
// EstadoBroker representa o estado de um broker
type EstadoBroker struct {
	ID                string             `json:"id"`
	LiderAtual        string             `json:"lider_atual"`
	Vizinhos          map[string]Vizinho `json:"vizinhos"`
	Recursos          map[string]Recurso `json:"recursos"`
	UltimaAtualizacao time.Time          `json:"ultima_atualizacao"`
	Versao            uint64             `json:"versao"`
}

// Vizinho representa um broker conhecido
type Vizinho struct {
	ID              string    `json:"id"`
	EnderecoTCP     string    `json:"endereco_tcp"`
	EnderecoUDP     string    `json:"endereco_udp"`
	UltimoBatimento time.Time `json:"ultimo_batimento"`
	Ativo           bool      `json:"ativo"`
	VersaoEstado    uint64    `json:"versao_estado"`
}

// Recurso representa um recurso gerenciado pelo sistema
type Recurso struct {
	ID             string    `json:"id"`
	Nome           string    `json:"nome"`
	Tipo           string    `json:"tipo"`
	Estado         string    `json:"estado"` // disponivel, em_uso, manutencao
	BrokerAtual    string    `json:"broker_atual"`
	BloqueadoPor   string    `json:"bloqueado_por"`   // ID do broker que tem o lock
	DonoRequisicao string    `json:"dono_requisicao"` // ID da requisicao que usou
	UltimoAcesso   time.Time `json:"ultimo_acesso"`
	Versao         uint64    `json:"versao"`
}

// Requisicao representa uma requisicao no sistema
type Requisicao struct {
	ID               string        `json:"id"`
	Tipo             string        `json:"tipo"`
	BrokerOrigem     string        `json:"broker_origem"`
	RecursoID        string        `json:"recurso_id"`
	Dados            interface{}   `json:"dados"`
	Estado           string        `json:"estado"` // pendente, concluido, em_andamento, falhou
	CarimboTempo     time.Time     `json:"carimbo_tempo"`
	Prioridade       int           `json:"prioridade"`       // 1=baixa, 5=alta
	GrauCriticidade  int           `json:"grau_criticidade"` // 1-5, onde 5 é mais crítico
	Tentativas       int           `json:"tentativas"`
	TimestampEntrada time.Time     `json:"timestamp_entrada"` // Quando entrou na fila
	SetorID          string        `json:"setor_id"`          // Setor que fez a requisição
	Timeout          time.Duration `json:"timeout"`           // Tempo máximo para atendimento
}

// Resposta representa uma resposta a uma requisicao
type Resposta struct {
	RequisicaoID string      `json:"requisicao_id"`
	Sucesso      bool        `json:"sucesso"`
	Dados        interface{} `json:"dados"`
	Erro         string      `json:"erro,omitempty"`
	CarimboTempo time.Time   `json:"carimbo_tempo"`
}

// SensorData representa os dados enviados por um sensor
type SensorData struct {
	ID           string    `json:"id"`
	Tipo         string    `json:"tipo"` // temperatura, pressao, movimento
	Valor        float64   `json:"valor"`
	Localizacao  string    `json:"localizacao"`
	CarimboTempo time.Time `json:"carimbo_tempo"`
	SetorID      string    `json:"setor_id"`
}

// Sensor representa um dispositivo sensor conectado ao sistema
type Sensor struct {
	ID            string    `json:"id"`
	SetorID       string    `json:"setor_id"`
	EnderecoTCP   string    `json:"endereco_tcp"` // usado para callback se necessÃ¡rio
	Tipo          string    `json:"tipo"`         // radar, boia, etc
	Localizacao   string    `json:"localizacao"`
	Conectado     bool      `json:"conectado"`
	UltimaLeitura time.Time `json:"ultima_leitura"`
}

// EventoSensor representa um evento crítico detectado por um sensor
type EventoSensor struct {
	ID           string      `json:"id"`
	TipoEvento   string      `json:"tipo_evento"` // BLOQUEIO_PARCIAL, EMBARCACAO_DERIVA, etc
	SensorID     string      `json:"sensor_id"`
	SetorID      string      `json:"setor_id"`
	Gravidade    int         `json:"gravidade"` // 1-5, onde 5 é mais crítico
	Descricao    string      `json:"descricao"`
	DadosRaw     interface{} `json:"dados_raw"`
	CarimboTempo time.Time   `json:"carimbo_tempo"`
	Processado   bool        `json:"processado"`
}

// MensagemDescoberta representa uma mensagem de descoberta de novos brokers
type MensagemDescoberta struct {
	Tipo         string     `json:"tipo"` // "DESCOBERTA", "ANUNCIO_NOVO_BROKER"
	OrigemID     string     `json:"origem_id"`
	BrokerInfo   BrokerInfo `json:"broker_info"`
	CarimboTempo time.Time  `json:"carimbo_tempo"`
}

// BrokerInfo contém informações de um broker para descoberta
type BrokerInfo struct {
	ID            string `json:"id"`
	EnderecoTCP   string `json:"endereco_tcp"`
	EnderecoUDP   string `json:"endereco_udp"`
	PortaControle string `json:"porta_controle"`
}

// NovaRequisicao cria uma nova requisição com valores padrão
func NovaRequisicao(tipo, brokerOrigem, recursoID string, prioridade, criticidade int) *Requisicao {
	return &Requisicao{
		ID:               fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), prioridade),
		Tipo:             tipo,
		BrokerOrigem:     brokerOrigem,
		RecursoID:        recursoID,
		Estado:           "pendente",
		CarimboTempo:     time.Now(),
		Prioridade:       prioridade,
		GrauCriticidade:  criticidade,
		Tentativas:       0,
		TimestampEntrada: time.Now(),
		Timeout:          30 * time.Second,
	}
}
