package broker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sistema-distribuido-brokers/internal/eleicao"
	"sistema-distribuido-brokers/internal/exclusao_mutua"
	"sistema-distribuido-brokers/internal/fila"
	"sistema-distribuido-brokers/internal/gossip"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"sync"
	"time"
)

// Broker representa um broker no sistema distribuído
type Broker struct {
	id                    string
	portaTCP              string
	portaUDP              string
	portaSensores         string // porta dedicada para conexões de sensores
	estado                *GerenciadorEstado
	gerenciadorRecursos   *GerenciadorRecursos
	algoritmoEleicao      *eleicao.AlgoritmoBully
	gerenciadorBatimentos *gossip.GerenciadorBatimentos
	protocoloGossip       *gossip.ProtocoloGossip
	filaDistribuida       *fila.FilaDistribuida
	mutexDistribuido      *exclusao_mutua.MutexDistribuido
	listenerTCP           net.Listener
	listenerSensores      net.Listener // listener separado para sensores (quando portaSensores != portaTCP)
	listenerUDP           *net.UDPConn
	droneEndpoints        map[string]string
	executando            bool
	mutex                 sync.RWMutex
	canalControle         chan struct{}

	// Controle de requisições para garantir idempotência
	requisicoesEmAndamento map[string]string // requisicaoID -> recursoID alocado
	requisicoesProcessadas map[string]bool   // requisicaoID -> foi concluída com sucesso
	requisicoesMutex       sync.RWMutex
}

// NovoBroker cria uma nova instância de Broker.
//
// portaCTRL é o endereço onde sensores se conectam. Se for diferente de portaTCP,
// o broker abre um listener TCP dedicado para sensores. Caso contrário, sensores
// e mensagens inter-broker compartilham portaTCP, distinguidos pelo formato do payload.
func NovoBroker(id, portaTCP, portaUDP, portaCTRL string, listaVizinhos []string, dronesConfig string) (*Broker, error) {
	estado := NovoGerenciadorEstado(id)
	estado.CarregarEstado()

	portaSensores := portaTCP
	if strings.TrimSpace(portaCTRL) != "" && portaCTRL != portaTCP {
		portaSensores = portaCTRL
	}

	b := &Broker{
		id:                     id,
		portaTCP:               portaTCP,
		portaUDP:               portaUDP,
		portaSensores:          portaSensores,
		estado:                 estado,
		gerenciadorRecursos:    NovoGerenciadorRecursos(id, estado, dronesConfig),
		filaDistribuida:        fila.NovaFilaDistribuida(id),
		droneEndpoints:         parseDrones(dronesConfig),
		executando:             true,
		canalControle:          make(chan struct{}),
		requisicoesProcessadas: make(map[string]bool),
		requisicoesEmAndamento: make(map[string]string),
	}

	// Inicializa vizinhos (formato da lista: id, endTCP, endUDP, id, endTCP, endUDP, ...)
	for i := 0; i+2 < len(listaVizinhos); i += 3 {
		b.estado.AtualizarVizinho(listaVizinhos[i], listaVizinhos[i+1], listaVizinhos[i+2])
	}

	vizinhos := b.estado.ObterVizinhosAtivos()

	b.algoritmoEleicao = eleicao.NovaEleicaoBully(id, vizinhos)
	b.mutexDistribuido = exclusao_mutua.NovoMutexDistribuido(id, vizinhos)
	b.mutexDistribuido.SetRecursoManager(b.gerenciadorRecursos)

	var err error
	b.gerenciadorBatimentos, err = gossip.NovoGerenciadorBatimentos(id, vizinhos, portaUDP)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar gerenciador de batimentos: %v", err)
	}

	b.protocoloGossip = gossip.NovoProtocoloGossip(id, estado.ObterEstado(), vizinhos)

	// Registra o handler de GOSSIP no gerenciador de batimentos.
	// Além de propagar o estado pelo protocolo gossip, sincroniza os recursos
	// locais a partir do estado recebido — CORREÇÃO: SincronizarRecursos agora
	// é chamado sempre que uma mensagem GOSSIP chega via UDP (PROBLEMA 6).
	b.gerenciadorBatimentos.SetGossipHandler(func(msg tipos.Mensagem) {
		b.protocoloGossip.ProcessarMensagemGossip(msg)

		if estadoRecebido, ok := extrairEstadoGossip(msg); ok {
			b.gerenciadorRecursos.SincronizarRecursos(estadoRecebido)
		}
	})

	return b, nil
}

// extrairEstadoGossip extrai o EstadoBroker contido em uma mensagem GOSSIP.
// Retorna (nil, false) se a mensagem não for GOSSIP ou não contiver estado válido.
func extrairEstadoGossip(msg tipos.Mensagem) (*tipos.EstadoBroker, bool) {
	if msg.Tipo != "GOSSIP" || msg.Dados == nil {
		return nil, false
	}
	dados, err := json.Marshal(msg.Dados)
	if err != nil {
		return nil, false
	}
	var estado tipos.EstadoBroker
	if err := json.Unmarshal(dados, &estado); err != nil {
		return nil, false
	}
	return &estado, true
}

// Iniciar inicia todos os serviços do broker
func (b *Broker) Iniciar() error {
	// Listener principal: mensagens inter-broker
	if err := b.iniciarListenerTCP(); err != nil {
		return fmt.Errorf("falha ao iniciar listener TCP: %v", err)
	}

	// Listener dedicado para sensores (apenas quando as portas diferem)
	if b.portaSensores != b.portaTCP {
		if err := b.iniciarListenerSensores(); err != nil {
			return fmt.Errorf("falha ao iniciar listener de sensores: %v", err)
		}
		utils.RegistrarLog("INFO", "Listener de sensores dedicado na porta %s", b.portaSensores)
	}

	b.gerenciadorBatimentos.Iniciar()
	b.protocoloGossip.Iniciar()

	go b.processarMensagensTCP()
	go b.processarRequisicoes()
	go b.monitorarEleicao()
	go b.monitorarFalhas()
	go b.limparRequisicoesAntigas()
	go b.replicacaoPeriodicaFila()

	// Pequeno atraso para deixar os vizinhos subirem antes de iniciar eleição
	time.Sleep(2 * time.Second)
	go b.algoritmoEleicao.IniciarEleicao()

	utils.RegistrarLog("INFO", "Broker %s iniciado (TCP=%s, UDP=%s, Sensores=%s)",
		b.id, b.portaTCP, b.portaUDP, b.portaSensores)

	return nil
}

// iniciarListenerTCP abre o listener TCP principal (inter-broker)
func (b *Broker) iniciarListenerTCP() error {
	listener, err := net.Listen("tcp", b.portaTCP)
	if err != nil {
		return err
	}
	b.listenerTCP = listener
	return nil
}

// iniciarListenerSensores abre um listener TCP dedicado para conexões de sensores
func (b *Broker) iniciarListenerSensores() error {
	listener, err := net.Listen("tcp", b.portaSensores)
	if err != nil {
		return err
	}
	b.listenerSensores = listener
	go b.aceitarConexoesSensores()
	return nil
}

// aceitarConexoesSensores aceita e roteia conexões no listener de sensores
func (b *Broker) aceitarConexoesSensores() {
	for b.executando {
		conn, err := b.listenerSensores.Accept()
		if err != nil {
			if b.executando {
				utils.RegistrarLog("ERRO", "Erro ao aceitar conexão de sensor: %v", err)
			}
			continue
		}
		go b.tratarConexaoSensorDedicado(conn)
	}
}

// tratarConexaoSensorDedicado lida com uma conexão vinda do listener dedicado de sensores.
// Neste listener, todos os payloads são de sensores — sem necessidade de discriminação.
func (b *Broker) tratarConexaoSensorDedicado(conn net.Conn) {
	defer conn.Close()
	leitor := bufio.NewReader(conn)

	for {
		linha, err := leitor.ReadBytes('\n')
		if err != nil {
			return
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(linha, &payload); err != nil {
			continue
		}
		b.processarSensorPayload(payload, conn)
	}
}

// processarMensagensTCP aceita conexões no listener inter-broker
func (b *Broker) processarMensagensTCP() {
	for b.executando {
		conexao, err := b.listenerTCP.Accept()
		if err != nil {
			if b.executando {
				utils.RegistrarLog("ERRO", "Erro ao aceitar conexão TCP: %v", err)
			}
			continue
		}
		go b.tratarConexaoTCP(conexao)
	}
}

// tratarConexaoTCP roteia mensagens recebidas no listener inter-broker.
// Quando portaSensores == portaTCP, sensores também chegam aqui e são
// identificados pelo formato do payload via ehMensagemSensor.
func (b *Broker) tratarConexaoTCP(conexao net.Conn) {
	defer conexao.Close()

	leitor := bufio.NewReader(conexao)
	for {
		linha, err := leitor.ReadBytes('\n')
		if err != nil {
			return
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(linha, &payload); err != nil {
			continue
		}

		// Mensagem de sensor detectada pelo formato do payload
		if b.ehMensagemSensor(payload) {
			b.processarSensorPayload(payload, conexao)
			continue
		}

		// Mensagem inter-broker
		var mensagem tipos.Mensagem
		if err := json.Unmarshal(linha, &mensagem); err != nil {
			continue
		}

		switch mensagem.Tipo {
		case "ELEICAO", "RESPOSTA_ELEICAO", "VITORIA":
			b.algoritmoEleicao.ProcessarMensagemEleicao(mensagem)

		case "SOLICITACAO_LOCK":
			b.tratarSolicitacaoLock(mensagem, conexao)

		case "LIBERACAO_LOCK":
			if recursoID, ok := extrairStringMapa(mensagem.Dados, "recurso_id"); ok {
				b.mutexDistribuido.LiberarAcesso(recursoID)
			}

		case "REQUISICAO":
			b.tratarRequisicao(mensagem)

		case "DRONE_DISPONIVEL":
			b.tratarDroneDisponivel(mensagem)

		case "SOLICITAR_FILA":
			b.tratarSolicitacaoFila(mensagem, conexao)

		case "SOLICITAR_REPLICA_FILA":
			b.tratarSolicitacaoReplicaFila(mensagem, conexao)

		case "REPLICAR_FILA":
			b.tratarReplicacaoFila(mensagem)

		case "GOSSIP", "BATIMENTO":
			// Chegam via UDP; ignorar silenciosamente no TCP
			continue

		default:
			utils.RegistrarLog("AVISO", "[BROKER-%s] mensagem desconhecida: %s", b.id, mensagem.Tipo)
		}
	}
}

// tratarSolicitacaoFila envia a fila atual para quem solicitou
func (b *Broker) tratarSolicitacaoFila(mensagem tipos.Mensagem, conn net.Conn) {
	dadosFila, err := b.filaDistribuida.SerializarFila()
	if err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao serializar fila: %v", b.id, err)
		return
	}

	resposta := tipos.Mensagem{
		Tipo:         "RESPOSTA_FILA",
		OrigemID:     b.id,
		DestinoID:    mensagem.OrigemID,
		Dados:        dadosFila,
		CarimboTempo: time.Now(),
	}

	if err := json.NewEncoder(conn).Encode(resposta); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao enviar fila: %v", b.id, err)
		return
	}
	utils.RegistrarLog("INFO", "[BROKER-%s] Fila enviada para %s", b.id, mensagem.OrigemID)
}

// tratarSolicitacaoReplicaFila envia réplica da fila quando um vizinho falha
func (b *Broker) tratarSolicitacaoReplicaFila(mensagem tipos.Mensagem, conn net.Conn) {
	vizinhoFalho, _ := extrairStringMapa(mensagem.Dados, "vizinho_falho")

	// Não envia réplica se o vizinho ainda estiver ativo
	if _, ativo := b.estado.ObterVizinhosAtivos()[vizinhoFalho]; ativo {
		return
	}

	dadosFila, err := b.filaDistribuida.SerializarFila()
	if err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao serializar réplica: %v", b.id, err)
		return
	}

	resposta := tipos.Mensagem{
		Tipo:         "RESPOSTA_REPLICA_FILA",
		OrigemID:     b.id,
		DestinoID:    mensagem.OrigemID,
		Dados:        dadosFila,
		CarimboTempo: time.Now(),
	}

	if err := json.NewEncoder(conn).Encode(resposta); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao enviar réplica: %v", b.id, err)
		return
	}
	utils.RegistrarLog("INFO", "[BROKER-%s] Réplica da fila enviada para %s", b.id, mensagem.OrigemID)
}

// tratarReplicacaoFila recebe e integra uma replicação de fila enviada por outro broker
func (b *Broker) tratarReplicacaoFila(mensagem tipos.Mensagem) {
	var dadosBytes []byte
	var err error

	switch v := mensagem.Dados.(type) {
	case []byte:
		dadosBytes = v
	case string:
		dadosBytes = []byte(v)
	case []interface{}, map[string]interface{}:
		dadosBytes, err = json.Marshal(v)
		if err != nil {
			utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao marshalar dados de replicação: %v", b.id, err)
			return
		}
	default:
		utils.RegistrarLog("ERRO", "[BROKER-%s] Tipo de dados de replicação não suportado: %T", b.id, mensagem.Dados)
		return
	}

	if err := b.filaDistribuida.DeserializarEAdicionarFila(dadosBytes); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao processar replicação: %v", b.id, err)
	} else {
		utils.RegistrarLog("DEBUG", "[BROKER-%s] Replicação de fila recebida de %s", b.id, mensagem.OrigemID)
	}
}

// replicacaoPeriodicaFila dispara replicação da fila a cada 10 segundos (apenas líder)
func (b *Broker) replicacaoPeriodicaFila() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !b.executando {
			return
		}
		if b.algoritmoEleicao.ObterLiderAtual() == b.id {
			b.replicarFilaParaVizinhos()
		}
	}
}

// tratarSolicitacaoLock processa uma solicitação de lock distribuído recebida de outro broker
func (b *Broker) tratarSolicitacaoLock(mensagem tipos.Mensagem, conn net.Conn) {
	aprovado := b.mutexDistribuido.ProcessarSolicitacaoLock(mensagem)

	resposta := tipos.Resposta{
		RequisicaoID: mensagem.OrigemID,
		Sucesso:      aprovado,
		CarimboTempo: time.Now(),
	}

	if err := json.NewEncoder(conn).Encode(resposta); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao enviar resposta de lock: %v", b.id, err)
	}
}

// tratarRequisicao enfileira uma requisição recebida de outro broker,
// rejeitando duplicatas antes de enfileirar
func (b *Broker) tratarRequisicao(mensagem tipos.Mensagem) {
	dados, ok := mensagem.Dados.(map[string]interface{})
	if !ok {
		return
	}

	requisicaoID := lerStringSeguro(dados, "requisicao_id")
	if requisicaoID == "" {
		requisicaoID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	b.requisicoesMutex.RLock()
	jaProcessada := b.requisicoesProcessadas[requisicaoID]
	jaAndamento := b.requisicoesEmAndamento[requisicaoID] != ""
	b.requisicoesMutex.RUnlock()

	if jaProcessada {
		utils.RegistrarLog("AVISO", "Requisicao %s ja foi processada, ignorando", requisicaoID)
		return
	}
	if jaAndamento {
		utils.RegistrarLog("INFO", "Requisicao %s ja esta em andamento", requisicaoID)
		return
	}

	prioridade := lerIntSeguro(dados, "prioridade")
	if prioridade == 0 {
		prioridade = 3
	}
	criticidade := lerIntSeguro(dados, "grau_criticidade")
	if criticidade == 0 {
		criticidade = prioridade
	}
	setorID := lerStringSeguro(dados, "setor_id")
	if setorID == "" {
		setorID = b.id
	}

	requisicao := &tipos.Requisicao{
		ID:               requisicaoID,
		Tipo:             lerStringSeguro(dados, "tipo"),
		BrokerOrigem:     mensagem.OrigemID,
		RecursoID:        lerStringSeguro(dados, "recurso_id"),
		Estado:           "pendente",
		CarimboTempo:     time.Now(),
		Prioridade:       prioridade,
		GrauCriticidade:  criticidade,
		Tentativas:       0,
		TimestampEntrada: time.Now(),
		SetorID:          setorID,
		Timeout:          30 * time.Second,
	}

	utils.RegistrarLog("INFO", "Nova requisicao %s: tipo=%s prioridade=%d criticidade=%d setor=%s",
		requisicao.ID, requisicao.Tipo, prioridade, criticidade, setorID)

	b.filaDistribuida.AdicionarRequisicao(requisicao)
}

// enviarRespostaTCP abre uma nova conexão TCP e envia a resposta serializada
func (b *Broker) enviarRespostaTCP(endereco string, resposta interface{}) error {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return err
	}
	defer conexao.Close()

	dados, err := json.Marshal(resposta)
	if err != nil {
		return err
	}
	_, err = conexao.Write(append(dados, '\n'))
	return err
}

// processarRequisicoes consome o canal de processamento respeitando a ordem de prioridade.
//
// Correções aplicadas:
//   - Verifica se a requisição ainda existe na fila antes de processar (evita "fantasmas")
//   - Após falha com back-off, usa RenotificarRequisicao em vez de looping direto
func (b *Broker) processarRequisicoes() {
	utils.RegistrarLog("INFO", "Iniciando processamento de fila no broker %s", b.id)

	for requisicao := range b.filaDistribuida.ObterCanalProcessamento() {
		// Requisição pode ter sido removida por timeout ou por outro goroutine
		if b.filaDistribuida.ObterPorID(requisicao.ID) == nil {
			utils.RegistrarLog("DEBUG", "Requisicao %s não encontrada na fila, ignorando", requisicao.ID)
			continue
		}

		// Verifica expiração
		if time.Since(requisicao.TimestampEntrada) > requisicao.Timeout {
			utils.RegistrarLog("AVISO", "Requisicao %s expirou antes do processamento", requisicao.ID)
			b.filaDistribuida.RemoverRequisicao(requisicao.ID)
			continue
		}

		// Não sou o líder: encaminha e remove da fila local
		if b.algoritmoEleicao.ObterLiderAtual() != b.id {
			utils.RegistrarLog("DEBUG", "Encaminhando requisicao %s para o lider %s",
				requisicao.ID, b.algoritmoEleicao.ObterLiderAtual())
			b.encaminharRequisicaoParaLider(requisicao)
			b.filaDistribuida.RemoverRequisicao(requisicao.ID)
			continue
		}

		b.executarRequisicao(requisicao)

		if requisicao.Estado == "concluido" {
			b.filaDistribuida.RemoverRequisicao(requisicao.ID)
		} else if requisicao.Estado == "pendente" && requisicao.Tentativas > 0 {
			b.filaDistribuida.ReordenarFila()
			// Back-off antes de recolocar no canal para evitar busy-loop
			go func(req *tipos.Requisicao) {
				time.Sleep(2 * time.Second)
				b.filaDistribuida.RenotificarRequisicao(req.ID)
			}(requisicao)
		}
	}
}

// executarRequisicao despacha a requisição para o handler correto,
// garantindo que a mesma requisição não seja processada em paralelo
func (b *Broker) executarRequisicao(req *tipos.Requisicao) {
	b.requisicoesMutex.Lock()
	if _, emAndamento := b.requisicoesEmAndamento[req.ID]; emAndamento {
		b.requisicoesMutex.Unlock()
		utils.RegistrarLog("AVISO", "Requisição %s já está em andamento, ignorando duplicata", req.ID)
		return
	}
	b.requisicoesEmAndamento[req.ID] = ""
	b.requisicoesMutex.Unlock()

	defer func() {
		b.requisicoesMutex.Lock()
		delete(b.requisicoesEmAndamento, req.ID)
		b.requisicoesMutex.Unlock()
	}()

	utils.RegistrarLog("INFO", "Processando requisição %s tipo=%s prioridade=%d",
		req.ID, req.Tipo, req.Prioridade)

	switch req.Tipo {
	case "ALOCAR_RECURSO", "ALOCAR_DRONE":
		b.processarAlocacaoDrone(req)
	case "LIBERAR_RECURSO":
		b.processarLiberacaoRecurso(req)
	case "CONSULTAR_RECURSOS":
		b.processarConsultaRecursos(req)
	case "GOSSIP", "BATIMENTO":
		return // Nunca devem chegar aqui
	default:
		utils.RegistrarLog("AVISO", "Tipo de requisicao desconhecido: %s", req.Tipo)
	}

	b.estado.SalvarEstado()
}

// processarAlocacaoDrone tenta alocar um drone disponível para a requisição,
// usando o mutex distribuído para garantir exclusão mútua entre brokers
func (b *Broker) processarAlocacaoDrone(req *tipos.Requisicao) {
	if strings.TrimSpace(req.RecursoID) == "" {
		req.RecursoID = b.proximoDroneDisponivel()
		utils.RegistrarLog("DEBUG", "Drone escolhido automaticamente: %s", req.RecursoID)
	}

	if req.RecursoID == "" {
		utils.RegistrarLog("AVISO", "Nenhum drone disponível para req %s (tentativa %d)",
			req.ID, req.Tentativas)
		req.Estado = "pendente"
		req.Tentativas++
		req.CarimboTempo = time.Now()
		_ = b.filaDistribuida.AdicionarRequisicao(req)
		return
	}

	b.requisicoesMutex.Lock()
	b.requisicoesEmAndamento[req.ID] = req.RecursoID
	b.requisicoesMutex.Unlock()

	aprovado, err := b.mutexDistribuido.SolicitarAcesso(req.RecursoID, req.ID)
	if err != nil {
		utils.RegistrarLog("ERRO", "Erro ao solicitar lock para %s: %v", req.RecursoID, err)
	}
	if !aprovado {
		utils.RegistrarLog("AVISO", "Lock negado para %s, req %s volta à fila", req.RecursoID, req.ID)
		req.Estado = "pendente"
		req.Tentativas++
		req.CarimboTempo = time.Now()
		_ = b.filaDistribuida.AdicionarRequisicao(req)
		return
	}

	recurso, sucesso, motivo := b.gerenciadorRecursos.TentarAlocarRecurso(req.RecursoID, req.ID, b.id)
	if !sucesso {
		utils.RegistrarLog("ERRO", "Falha ao alocar %s: %s", req.RecursoID, motivo)
		b.mutexDistribuido.LiberarAcesso(req.RecursoID)
		req.Estado = "pendente"
		req.Tentativas++
		req.CarimboTempo = time.Now()
		_ = b.filaDistribuida.AdicionarRequisicao(req)
		return
	}

	if err := b.comandarDrone(recurso.ID, req); err != nil {
		utils.RegistrarLog("ERRO", "Falha ao comandar drone %s: %v", recurso.ID, err)
		b.gerenciadorRecursos.LiberarRecurso(req.RecursoID)
		b.mutexDistribuido.LiberarAcesso(req.RecursoID)
		req.Estado = "pendente"
		req.Tentativas++
		req.CarimboTempo = time.Now()
		_ = b.filaDistribuida.AdicionarRequisicao(req)
		return
	}

	b.requisicoesMutex.Lock()
	b.requisicoesProcessadas[req.ID] = true
	b.requisicoesMutex.Unlock()

	req.Estado = "concluido"
	utils.RegistrarLog("INFO", "[BROKER-%s] drone %s alocado para req %s (prioridade %d)",
		b.id, recurso.ID, req.ID, req.Prioridade)

	// Segurança: libera o lock se a requisição tentou muitas vezes
	if req.Tentativas > 10 {
		b.mutexDistribuido.LiberarAcesso(req.RecursoID)
	}
}

// processarLiberacaoRecurso libera o recurso especificado na requisição
func (b *Broker) processarLiberacaoRecurso(req *tipos.Requisicao) {
	if err := b.gerenciadorRecursos.LiberarRecurso(req.RecursoID); err != nil {
		utils.RegistrarLog("ERRO", "Falha ao liberar recurso %s: %v", req.RecursoID, err)
	}
	b.mutexDistribuido.LiberarAcesso(req.RecursoID)
	req.Estado = "concluido"

	b.requisicoesMutex.Lock()
	b.requisicoesProcessadas[req.ID] = true
	delete(b.requisicoesEmAndamento, req.ID)
	b.requisicoesMutex.Unlock()

	utils.RegistrarLog("INFO", "Recurso %s liberado para req %s", req.RecursoID, req.ID)
}

// processarConsultaRecursos responde com a lista de recursos disponíveis
func (b *Broker) processarConsultaRecursos(req *tipos.Requisicao) {
	recursos := b.gerenciadorRecursos.ObterRecursosDisponiveis()
	req.Dados = recursos
	req.Estado = "concluido"

	b.requisicoesMutex.Lock()
	b.requisicoesProcessadas[req.ID] = true
	delete(b.requisicoesEmAndamento, req.ID)
	b.requisicoesMutex.Unlock()

	utils.RegistrarLog("INFO", "Consulta de recursos: %d drones disponíveis", len(recursos))
}

// limparRequisicoesAntigas remove entradas antigas dos mapas de controle de requisições
func (b *Broker) limparRequisicoesAntigas() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.requisicoesMutex.Lock()

		if len(b.requisicoesProcessadas) > 10000 {
			count := 0
			for k := range b.requisicoesProcessadas {
				delete(b.requisicoesProcessadas, k)
				count++
				if count >= 5000 {
					break
				}
			}
			utils.RegistrarLog("INFO", "Limpeza: %d requisições processadas removidas", count)
		}

		if len(b.requisicoesEmAndamento) > 5000 {
			count := 0
			for k := range b.requisicoesEmAndamento {
				delete(b.requisicoesEmAndamento, k)
				count++
				if count >= 2500 {
					break
				}
			}
			utils.RegistrarLog("INFO", "Limpeza: %d requisições em andamento removidas", count)
		}

		b.requisicoesMutex.Unlock()
	}
}

// encaminharRequisicaoParaLider encaminha uma requisição para o broker líder via TCP
func (b *Broker) encaminharRequisicaoParaLider(req *tipos.Requisicao) {
	liderID := b.algoritmoEleicao.ObterLiderAtual()
	if liderID == "" || liderID == b.id {
		return
	}

	if lider, existe := b.estado.ObterVizinhosAtivos()[liderID]; existe {
		mensagem := tipos.Mensagem{
			Tipo:         "REQUISICAO",
			OrigemID:     b.id,
			DestinoID:    liderID,
			Dados:        req,
			CarimboTempo: time.Now(),
		}
		if err := b.enviarRespostaTCP(lider.EnderecoTCP, mensagem); err != nil {
			utils.RegistrarLog("ERRO", "Falha ao encaminhar req %s para líder %s: %v",
				req.ID, liderID, err)
		}
	}
}

// monitorarEleicao aguarda resultados de eleição e atualiza o estado de liderança
func (b *Broker) monitorarEleicao() {
	for resultado := range b.algoritmoEleicao.ObterCanalResultado() {
		b.estado.AtualizarLider(resultado)
		utils.RegistrarLog("INFO", "Novo líder eleito: %s", resultado)
	}
}

// monitorarFalhas reage a falhas de vizinhos detectadas pelo gerenciador de batimentos
func (b *Broker) monitorarFalhas() {
	utils.RegistrarLog("INFO", "Iniciando monitoramento de falhas no broker %s", b.id)

	for vizinhoID := range b.gerenciadorBatimentos.ObterCanalFalha() {
		utils.RegistrarLog("AVISO", "[BROKER-%s] vizinho %s falhou", b.id, vizinhoID)
		b.estado.MarcarVizinhoInativo(vizinhoID)

		if b.algoritmoEleicao.ObterLiderAtual() == b.id {
			utils.RegistrarLog("INFO", "Líder %s assumindo requisições do vizinho falho %s",
				b.id, vizinhoID)
			go b.recuperarFilaDoVizinhoFalho(vizinhoID)
			go b.liberarDronesDoVizinhoComTimeout(vizinhoID, 10*time.Second)
		}

		if b.algoritmoEleicao.ObterLiderAtual() == vizinhoID {
			utils.RegistrarLog("AVISO", "Líder %s falhou, iniciando nova eleição", vizinhoID)
			go b.algoritmoEleicao.IniciarEleicao()
		}

		go b.replicarFilaParaVizinhos()
	}
}

// recuperarFilaDoVizinhoFalho tenta obter a fila de um vizinho que falhou,
// consultando primeiro o próprio vizinho (pode ter se recuperado) e depois os demais
func (b *Broker) recuperarFilaDoVizinhoFalho(vizinhoID string) {
	utils.RegistrarLog("INFO", "[BROKER-%s] Tentando recuperar fila do vizinho falho %s",
		b.id, vizinhoID)

	enderecoTCP := b.obterEnderecoVizinho(vizinhoID)
	if enderecoTCP == "" {
		utils.RegistrarLog("AVISO", "[BROKER-%s] Endereço do vizinho %s não encontrado",
			b.id, vizinhoID)
		return
	}

	conn, err := net.DialTimeout("tcp", enderecoTCP, 3*time.Second)
	if err != nil {
		utils.RegistrarLog("AVISO", "[BROKER-%s] Vizinho %s offline, buscando réplica da fila",
			b.id, vizinhoID)
		b.buscarReplicaFila(vizinhoID)
		return
	}
	defer conn.Close()

	mensagem := tipos.Mensagem{
		Tipo:         "SOLICITAR_FILA",
		OrigemID:     b.id,
		DestinoID:    vizinhoID,
		CarimboTempo: time.Now(),
	}

	if err := json.NewEncoder(conn).Encode(mensagem); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao solicitar fila: %v", b.id, err)
		return
	}

	var resposta tipos.Mensagem
	if err := json.NewDecoder(conn).Decode(&resposta); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao receber fila: %v", b.id, err)
		return
	}

	if resposta.Tipo == "RESPOSTA_FILA" && resposta.Dados != nil {
		if dadosBytes, ok := resposta.Dados.([]byte); ok {
			if err := b.filaDistribuida.DeserializarEAdicionarFila(dadosBytes); err != nil {
				utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao deserializar fila: %v", b.id, err)
			} else {
				utils.RegistrarLog("INFO", "[BROKER-%s] Fila recuperada do vizinho %s",
					b.id, vizinhoID)
			}
		}
	}
}

// buscarReplicaFila busca uma réplica da fila do vizinho falho em outros brokers ativos
func (b *Broker) buscarReplicaFila(vizinhoID string) {
	for id, vizinho := range b.estado.ObterVizinhosAtivos() {
		if id == vizinhoID || id == b.id {
			continue
		}

		conn, err := net.DialTimeout("tcp", vizinho.EnderecoTCP, 3*time.Second)
		if err != nil {
			continue
		}

		mensagem := tipos.Mensagem{
			Tipo:         "SOLICITAR_REPLICA_FILA",
			OrigemID:     b.id,
			DestinoID:    id,
			Dados:        map[string]string{"vizinho_falho": vizinhoID},
			CarimboTempo: time.Now(),
		}

		if err := json.NewEncoder(conn).Encode(mensagem); err != nil {
			conn.Close()
			continue
		}

		var resposta tipos.Mensagem
		if err := json.NewDecoder(conn).Decode(&resposta); err != nil {
			conn.Close()
			continue
		}

		if resposta.Tipo == "RESPOSTA_REPLICA_FILA" && resposta.Dados != nil {
			if dadosBytes, ok := resposta.Dados.([]byte); ok {
				if err := b.filaDistribuida.DeserializarEAdicionarFila(dadosBytes); err != nil {
					utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao restaurar réplica: %v", b.id, err)
				} else {
					utils.RegistrarLog("INFO", "[BROKER-%s] Fila restaurada de réplica do broker %s",
						b.id, id)
				}
			}
		}
		conn.Close()
	}
}

// replicarFilaParaVizinhos envia a fila atual para todos os vizinhos ativos.
// Executado apenas pelo líder.
func (b *Broker) replicarFilaParaVizinhos() {
	if b.algoritmoEleicao.ObterLiderAtual() != b.id {
		return
	}

	dadosFila, err := b.filaDistribuida.SerializarFila()
	if err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao serializar fila para replicação: %v", b.id, err)
		return
	}

	for id, vizinho := range b.estado.ObterVizinhosAtivos() {
		if id == b.id {
			continue
		}
		go func(vizinhoID, endereco string, dados []byte) {
			conn, err := net.DialTimeout("tcp", endereco, 2*time.Second)
			if err != nil {
				return
			}
			defer conn.Close()

			mensagem := tipos.Mensagem{
				Tipo:         "REPLICAR_FILA",
				OrigemID:     b.id,
				DestinoID:    vizinhoID,
				Dados:        dados,
				CarimboTempo: time.Now(),
			}
			if err := json.NewEncoder(conn).Encode(mensagem); err != nil {
				utils.RegistrarLog("ERRO", "[BROKER-%s] Erro ao replicar fila para %s: %v",
					b.id, vizinhoID, err)
			}
		}(id, vizinho.EnderecoTCP, dadosFila)
	}
}

// obterEnderecoVizinho retorna o endereço TCP de um vizinho ativo pelo ID
func (b *Broker) obterEnderecoVizinho(id string) string {
	if vizinho, ok := b.estado.ObterVizinhosAtivos()[id]; ok {
		return vizinho.EnderecoTCP
	}
	return ""
}

// Parar interrompe o broker de forma ordenada, fechando todos os listeners e canais
func (b *Broker) Parar() {
	b.executando = false

	if b.listenerTCP != nil {
		b.listenerTCP.Close()
	}
	if b.listenerSensores != nil {
		b.listenerSensores.Close()
	}

	b.gerenciadorBatimentos.Parar()
	b.protocoloGossip.Parar()
	b.filaDistribuida.Parar()
	b.estado.SalvarEstado()

	close(b.canalControle)
	utils.RegistrarLog("INFO", "Broker %s parado", b.id)
}

// ObterID retorna o ID do broker
func (b *Broker) ObterID() string {
	return b.id
}

// ObterLiderAtual retorna o ID do líder atual segundo o algoritmo de eleição
func (b *Broker) ObterLiderAtual() string {
	return b.algoritmoEleicao.ObterLiderAtual()
}

// ObterEstadoFila retorna um resumo do estado da fila para diagnóstico
func (b *Broker) ObterEstadoFila() map[string]interface{} {
	return map[string]interface{}{
		"tamanho":      b.filaDistribuida.Tamanho(),
		"requisicoes":  b.filaDistribuida.ListarRequisicoes(),
		"processadas":  len(b.requisicoesProcessadas),
		"em_andamento": len(b.requisicoesEmAndamento),
	}
}

// ehMensagemSensor retorna true se o payload JSON veio de um sensor.
//
// Sensores NÃO incluem "origem_id" (campo exclusivo de mensagens inter-broker),
// mas sempre incluem "id" e "tipo".
func (b *Broker) ehMensagemSensor(payload map[string]interface{}) bool {
	if _, temOrigemID := payload["origem_id"]; temOrigemID {
		return false
	}
	_, temID := payload["id"]
	_, temTipo := payload["tipo"]
	return temID && temTipo
}

// processarSensorPayload processa dados de telemetria recebidos de um sensor.
//
// Correções aplicadas (PROBLEMA 4):
//   - Todos os json.Encoder.Encode verificam e logam erros
//   - SetWriteDeadline protege contra conexões lentas/travadas
//   - Evento crítico só é enfileirado APÓS confirmação do ACK
//   - Mensagem de registro (sem "valor") responde e retorna imediatamente
func (b *Broker) processarSensorPayload(payload map[string]interface{}, conn net.Conn) {
	_, temValor := payload["valor"]

	// Handshake de registro: sensor enviou apenas seus metadados, sem leitura
	if !temValor {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		resposta := map[string]string{"status": "registrado", "setor_id": b.id}
		if err := json.NewEncoder(conn).Encode(resposta); err != nil {
			utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao confirmar registro de sensor: %v",
				b.id, err)
		}
		conn.SetWriteDeadline(time.Time{})
		return
	}

	tipo := strings.ToLower(lerStringSeguro(payload, "tipo"))
	valor := lerFloatSeguro(payload, "valor")
	gravidade := 0

	switch tipo {
	case "movimento":
		if valor > 0.8 {
			gravidade = 5
		}
	case "pressao":
		if valor < 980 || valor > 1050 {
			gravidade = 4
		}
	case "temperatura":
		if valor > 45 || valor < -10 {
			gravidade = 3
		}
	}

	// Envia ACK ao sensor com deadline para não bloquear em conexão lenta
	ack := map[string]interface{}{
		"status":         "recebido",
		"timestamp":      time.Now(),
		"broker_id":      b.id,
		"dados_id":       payload["id"],
		"evento_critico": gravidade > 0,
	}

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(ack); err != nil {
		// ACK não entregue: não processa o evento para manter consistência
		utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao enviar ACK (tipo=%s valor=%.2f): %v",
			b.id, tipo, valor, err)
		conn.SetWriteDeadline(time.Time{})
		return
	}
	conn.SetWriteDeadline(time.Time{})

	// Telemetria normal: apenas loga
	if gravidade == 0 {
		utils.RegistrarLog("DEBUG", "[BROKER-%s] telemetria normal: %s = %.2f", b.id, tipo, valor)
		return
	}

	// Evento crítico: cria e enfileira requisição de alocação de drone
	requisicaoID := fmt.Sprintf("req-%d", time.Now().UnixNano())

	b.requisicoesMutex.Lock()
	b.requisicoesEmAndamento[requisicaoID] = ""
	b.requisicoesMutex.Unlock()

	requisicao := &tipos.Requisicao{
		ID:               requisicaoID,
		Tipo:             "ALOCAR_DRONE",
		BrokerOrigem:     b.id,
		Estado:           "pendente",
		CarimboTempo:     time.Now(),
		Prioridade:       gravidade,
		GrauCriticidade:  gravidade,
		Tentativas:       0,
		TimestampEntrada: time.Now(),
		Timeout:          30 * time.Second,
		Dados:            payload,
	}

	if err := b.filaDistribuida.AdicionarRequisicao(requisicao); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao enfileirar req %s: %v",
			b.id, requisicaoID, err)
		return
	}

	utils.RegistrarLog("ALERTA", "[BROKER-%s] evento crítico -> req=%s tipo=%s valor=%.2f criticidade=%d",
		b.id, requisicaoID, tipo, valor, gravidade)
}

// proximoDroneDisponivel retorna o ID do primeiro drone disponível
func (b *Broker) proximoDroneDisponivel() string {
	for _, r := range b.gerenciadorRecursos.ObterRecursosDisponiveis() {
		if strings.HasPrefix(r.ID, "drone-") {
			return r.ID
		}
	}
	return ""
}

// comandarDrone envia o comando START_MISSION ao drone via TCP.
// Se o drone não tiver endpoint configurado, considera a missão simulada (ambiente de teste).
func (b *Broker) comandarDrone(droneID string, req *tipos.Requisicao) error {
	endereco := b.droneEndpoints[droneID]
	if strings.TrimSpace(endereco) == "" {
		utils.RegistrarLog("AVISO", "[BROKER-%s] Drone %s sem endpoint, simulando missão", b.id, droneID)
		return nil
	}

	conexao, err := net.DialTimeout("tcp", endereco, 3*time.Second)
	if err != nil {
		return fmt.Errorf("falha ao conectar ao drone %s em %s: %v", droneID, endereco, err)
	}
	defer conexao.Close()

	msg := map[string]interface{}{
		"tipo":            "START_MISSION",
		"drone_id":        droneID,
		"requisicao_id":   req.ID,
		"setor_id":        b.id,
		"callback_broker": b.enderecoCallback(),
		"carimbo_tempo":   time.Now(),
	}

	dados, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("falha ao serializar comando para drone %s: %v", droneID, err)
	}

	conexao.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err = conexao.Write(append(dados, '\n')); err != nil {
		return fmt.Errorf("falha ao enviar comando para drone %s: %v", droneID, err)
	}

	return nil
}

// tratarDroneDisponivel processa a notificação de que um drone completou a missão
func (b *Broker) tratarDroneDisponivel(msg tipos.Mensagem) {
	droneID, ok := extrairStringMapa(msg.Dados, "drone_id")
	if !ok {
		utils.RegistrarLog("AVISO", "[BROKER-%s] DRONE_DISPONIVEL sem campo drone_id", b.id)
		return
	}
	if err := b.gerenciadorRecursos.LiberarRecurso(droneID); err != nil {
		utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao liberar drone %s: %v", b.id, droneID, err)
		return
	}
	b.mutexDistribuido.LiberarAcesso(droneID)
	utils.RegistrarLog("INFO", "[BROKER-%s] %s voltou para DISPONIVEL", b.id, droneID)
}

// liberarDronesDoVizinhoComTimeout libera, após o timeout, drones presos em um vizinho falho
// e recria as requisições correspondentes para serem reprocessadas
func (b *Broker) liberarDronesDoVizinhoComTimeout(vizinhoID string, timeout time.Duration) {
	time.Sleep(timeout)

	estado := b.estado.ObterEstado()
	requisicoesTransferidas := 0

	for id, recurso := range estado.Recursos {
		if recurso.Tipo != "drone" || recurso.BrokerAtual != vizinhoID || recurso.Estado == "disponivel" {
			continue
		}

		if err := b.gerenciadorRecursos.LiberarRecurso(id); err != nil {
			utils.RegistrarLog("ERRO", "[BROKER-%s] Falha ao liberar drone %s: %v", b.id, id, err)
			continue
		}

		b.mutexDistribuido.LiberarAcesso(id)
		utils.RegistrarLog("AVISO", "[BROKER-%s] timeout liberou drone %s preso em %s",
			b.id, id, vizinhoID)

		if recurso.DonoRequisicao != "" {
			req := &tipos.Requisicao{
				ID:               recurso.DonoRequisicao + "-retry",
				Tipo:             "ALOCAR_RECURSO",
				BrokerOrigem:     b.id,
				RecursoID:        id,
				Estado:           "pendente",
				Prioridade:       5,
				GrauCriticidade:  5,
				Tentativas:       0,
				TimestampEntrada: time.Now(),
				Timeout:          30 * time.Second,
			}
			if err := b.filaDistribuida.AdicionarRequisicao(req); err == nil {
				requisicoesTransferidas++
			}
		}
	}

	if requisicoesTransferidas > 0 {
		utils.RegistrarLog("INFO", "[BROKER-%s] %d requisições transferidas do vizinho falho %s",
			b.id, requisicoesTransferidas, vizinhoID)
	}
}

// parseDrones converte a string de configuração de endpoints de drones em um mapa ID->endereço.
// Formato: "drone-01=host:porta,drone-02=host:porta,..."
func parseDrones(raw string) map[string]string {
	res := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return res
	}
	for _, p := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 {
			res[kv[0]] = kv[1]
		}
	}
	return res
}

// enderecoCallback retorna o endereço TCP deste broker para que drones possam notificá-lo
func (b *Broker) enderecoCallback() string {
	if cb := strings.TrimSpace(os.Getenv("BROKER_CALLBACK")); cb != "" {
		return cb
	}
	host := strings.TrimSpace(os.Getenv("BROKER_HOST"))
	if host == "" {
		host = "localhost"
	}
	porta := strings.TrimPrefix(b.portaTCP, ":")
	return host + ":" + porta
}

// ─── Funções auxiliares de extração tipada ────────────────────────────────────

func lerStringSeguro(m map[string]interface{}, k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func lerIntSeguro(m map[string]interface{}, k string) int {
	v, ok := m[k]
	if !ok {
		return 0
	}
	if f, ok := v.(float64); ok {
		return int(f)
	}
	if i, ok := v.(int); ok {
		return i
	}
	return 0
}

func lerFloatSeguro(m map[string]interface{}, k string) float64 {
	v, ok := m[k]
	if !ok {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func extrairStringMapa(dados interface{}, chave string) (string, bool) {
	m, ok := dados.(map[string]interface{})
	if !ok {
		return "", false
	}
	v, ok := m[chave]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
