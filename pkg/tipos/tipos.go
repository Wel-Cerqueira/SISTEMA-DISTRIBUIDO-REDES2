package tipos

import (
	"sync"
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
type EstadoBroker struct {
	ID                string             `json:"id"`
	LiderAtual        string             `json:"lider_atual"`
	Vizinhos          map[string]Vizinho `json:"vizinhos"`
	Recursos          map[string]Recurso `json:"recursos"`
	UltimaAtualizacao time.Time          `json:"ultima_atualizacao"`
	Versao            uint64             `json:"versao"`
	sync.RWMutex
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
	ID            string    `json:"id"`
	Nome          string    `json:"nome"`
	Tipo          string    `json:"tipo"`
	Estado        string    `json:"estado"`
	BrokerAtual string    `json:"broker_atual"`
	BloqueadoPor  string    `json:"bloqueado_por"`
	UltimoAcesso  time.Time `json:"ultimo_acesso"`
	Versao        uint64    `json:"versao"`
}

// Requisicao representa uma requisiÃ§Ã£o no sistema
type Requisicao struct {
	ID             string      `json:"id"`
	Tipo           string      `json:"tipo"`
	BrokerOrigem string      `json:"broker_origem"`
	RecursoID      string      `json:"recurso_id"`
	Dados          interface{} `json:"dados"`
	Estado         string      `json:"estado"`
	CarimboTempo   time.Time   `json:"carimbo_tempo"`
	Prioridade     int         `json:"prioridade"`
	GrauCriticidade int        `json:"grau_criticidade"`
	Tentativas     int         `json:"tentativas"`
}

// Resposta representa uma resposta a uma requisiÃ§Ã£o
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

// EventoSensor representa um evento crÃ­tico detectado por um sensor
type EventoSensor struct {
	ID           string      `json:"id"`
	TipoEvento   string      `json:"tipo_evento"` // BLOQUEIO_PARCIAL, EMBARCACAO_DERIVA, etc
	SensorID     string      `json:"sensor_id"`
	SetorID      string      `json:"setor_id"`
	Gravidade    int         `json:"gravidade"` // 1-5, onde 5 Ã© mais crÃ­tico
	Descricao    string      `json:"descricao"`
	DadosRaw     interface{} `json:"dados_raw"`
	CarimboTempo time.Time   `json:"carimbo_tempo"`
	Processado   bool        `json:"processado"`
}

