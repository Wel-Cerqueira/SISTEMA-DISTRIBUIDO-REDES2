package corretor

import (
	"encoding/json"
	"fmt"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// GerenciadorSensores gerencia conexões e dados de sensores
type GerenciadorSensores struct {
	idCorretor       string
	sensores         map[string]*tipos.Sensor
	ultimosDados     map[string]*tipos.SensorData
	eventosPendentes []*tipos.EventoSensor
	mutex            sync.RWMutex
	listenerTCP      net.Listener
	executando       bool
}

// NovoGerenciadorSensores cria um novo gerenciador de sensores
func NovoGerenciadorSensores(idCorretor string) *GerenciadorSensores {
	return &GerenciadorSensores{
		idCorretor:       idCorretor,
		sensores:         make(map[string]*tipos.Sensor),
		ultimosDados:     make(map[string]*tipos.SensorData),
		eventosPendentes: make([]*tipos.EventoSensor, 0),
		executando:       true,
	}
}

// Iniciar inicia o listener para conexões de sensores
func (gs *GerenciadorSensores) Iniciar(portaSensorTCP string) error {
	listener, err := net.Listen("tcp", portaSensorTCP)
	if err != nil {
		return err
	}
	gs.listenerTCP = listener
	go gs.aceitarConexoes()
	utils.RegistrarLog("INFO", "Gerenciador de Sensores iniciado na porta %s", portaSensorTCP)
	return nil
}

// aceitarConexoes aceita conexões de sensores
func (gs *GerenciadorSensores) aceitarConexoes() {
	for gs.executando {
		conexao, err := gs.listenerTCP.Accept()
		if err != nil {
			if gs.executando {
				utils.RegistrarLog("ERRO", "Erro ao aceitar conexão de sensor: %v", err)
			}
			continue
		}
		go gs.tratarSensor(conexao)
	}
}

// tratarSensor processa a conexão de um sensor
func (gs *GerenciadorSensores) tratarSensor(conexao net.Conn) {
	defer conexao.Close()

	decoder := json.NewDecoder(conexao)

	// Primeiro, o sensor deve se registrar
	var registro tipos.Sensor
	if err := decoder.Decode(&registro); err != nil {
		utils.RegistrarLog("ERRO", "Erro ao decodificar registro do sensor: %v", err)
		return
	}

	// Registra o sensor
	gs.mutex.Lock()
	registro.Conectado = true
	registro.UltimaLeitura = time.Now()
	gs.sensores[registro.ID] = &registro
	gs.mutex.Unlock()

	utils.RegistrarLog("INFO", "Sensor %s (%s) registrado no setor %s",
		registro.ID, registro.Tipo, gs.idCorretor)

	// Responde com confirmação
	resposta := map[string]string{"status": "registrado", "setor_id": gs.idCorretor}
	json.NewEncoder(conexao).Encode(resposta)

	// Agora processa os dados enviados pelo sensor
	for gs.executando {
		var dados tipos.SensorData
		if err := decoder.Decode(&dados); err != nil {
			utils.RegistrarLog("ERRO", "Erro ao decodificar dados do sensor %s: %v",
				registro.ID, err)
			return
		}

		gs.processarDadosSensor(&dados)
	}
}

// processarDadosSensor processa os dados recebidos do sensor
func (gs *GerenciadorSensores) processarDadosSensor(dados *tipos.SensorData) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	// Atualiza último dado
	gs.ultimosDados[dados.ID] = dados

	// Atualiza timestamp do sensor
	if sensor, existe := gs.sensores[dados.ID]; existe {
		sensor.UltimaLeitura = dados.CarimboTempo
	}

	utils.RegistrarLog("DEBUG", "Sensor %s: %s = %.2f em %s",
		dados.ID, dados.Tipo, dados.Valor, dados.Localizacao)

	// Verifica se os dados representam um evento crítico
	evento := gs.analisarEventoCritico(dados)
	if evento != nil {
		gs.eventosPendentes = append(gs.eventosPendentes, evento)
		utils.RegistrarLog("ALERTA", "Evento crítico detectado: %s (gravidade %d)",
			evento.TipoEvento, evento.Gravidade)
	}
}

// analisarEventoCritico analisa se os dados representam um evento crítico
func (gs *GerenciadorSensores) analisarEventoCritico(dados *tipos.SensorData) *tipos.EventoSensor {
	var evento *tipos.EventoSensor

	switch dados.Tipo {
	case "movimento":
		// Detecção de objeto não identificado
		if dados.Valor > 0.8 {
			evento = &tipos.EventoSensor{
				ID:           gerarID(),
				TipoEvento:   "OBJETO_NAO_IDENTIFICADO",
				SensorID:     dados.ID,
				SetorID:      gs.idCorretor,
				Gravidade:    4,
				Descricao:    "Objeto não identificado detectado",
				DadosRaw:     dados,
				CarimboTempo: time.Now(),
				Processado:   false,
			}
		}
	case "pressao":
		// Variação anormal de pressão (tempestade)
		if dados.Valor < 980 || dados.Valor > 1050 {
			evento = &tipos.EventoSensor{
				ID:           gerarID(),
				TipoEvento:   "CONDICAO_CLIMATICA_SEVERA",
				SensorID:     dados.ID,
				SetorID:      gs.idCorretor,
				Gravidade:    3,
				Descricao:    "Condição climática severa detectada",
				DadosRaw:     dados,
				CarimboTempo: time.Now(),
				Processado:   false,
			}
		}
	case "temperatura":
		// Temperatura extrema
		if dados.Valor > 45 || dados.Valor < -10 {
			evento = &tipos.EventoSensor{
				ID:           gerarID(),
				TipoEvento:   "TEMPERATURA_EXTREMA",
				SensorID:     dados.ID,
				SetorID:      gs.idCorretor,
				Gravidade:    2,
				Descricao:    "Temperatura extrema detectada",
				DadosRaw:     dados,
				CarimboTempo: time.Now(),
				Processado:   false,
			}
		}
	}

	return evento
}

// ObterEventosPendentes retorna e limpa os eventos pendentes
func (gs *GerenciadorSensores) ObterEventosPendentes() []*tipos.EventoSensor {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	eventos := gs.eventosPendentes
	gs.eventosPendentes = make([]*tipos.EventoSensor, 0)
	return eventos
}

// Parar finaliza o gerenciador de sensores
func (gs *GerenciadorSensores) Parar() {
	gs.executando = false
	if gs.listenerTCP != nil {
		gs.listenerTCP.Close()
	}
	utils.RegistrarLog("INFO", "Gerenciador de sensores parado")
}

// gerarID gera um ID único
func gerarID() string {
	return fmt.Sprintf("evt-%d", time.Now().UnixNano())
}
