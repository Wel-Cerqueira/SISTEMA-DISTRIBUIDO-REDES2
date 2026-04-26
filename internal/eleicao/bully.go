package eleicao

import (
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"sync"
	"time"
)

// AlgoritmoBully implementa o algoritmo de eleiÃ§Ã£o Bully
type AlgoritmoBully struct {
	idBroker          string
	liderAtual          string
	vizinhos            map[string]*tipos.Vizinho
	emEleicao           bool
	canalEleicao        chan tipos.Mensagem
	canalResultado      chan string
	mutex               sync.RWMutex
	tempoEsperaResposta time.Duration
	tempoEsperaVitoria  time.Duration
}

// NovaEleicaoBully cria uma nova instÃ¢ncia do algoritmo Bully
func NovaEleicaoBully(idBroker string, vizinhos map[string]*tipos.Vizinho) *AlgoritmoBully {
	return &AlgoritmoBully{
		idBroker:          idBroker,
		liderAtual:          "",
		vizinhos:            vizinhos,
		emEleicao:           false,
		canalEleicao:        make(chan tipos.Mensagem, 100),
		canalResultado:      make(chan string, 10),
		tempoEsperaResposta: 5 * time.Second,  // Aumentado para evitar race conditions
		tempoEsperaVitoria:  8 * time.Second,  // Aumentado para evitar race conditions
	}
}

// IniciarEleicao inicia o processo de eleiÃ§Ã£o
func (ab *AlgoritmoBully) IniciarEleicao() {
	ab.mutex.Lock()
	if ab.emEleicao {
		ab.mutex.Unlock()
		return
	}
	ab.emEleicao = true
	ab.mutex.Unlock()

	utils.RegistrarLog("INFO", "Broker %s iniciando eleiÃ§Ã£o", ab.idBroker)

	// Espera um tempo aleatório para evitar sincronização de eleições
	time.Sleep(time.Duration(100+time.Now().UnixNano()%500) * time.Millisecond)

	// Encontra brokers com ID maior
	brokersMaiores := ab.encontrarBrokersMaiores()

	if len(brokersMaiores) == 0 {
		// Nenhum broker maior, verifica se já não há um líder
		if ab.liderAtual == "" {
			ab.declararVitoria()
		} else {
			ab.mutex.Lock()
			ab.emEleicao = false
			ab.mutex.Unlock()
			utils.RegistrarLog("INFO", "LÃ­der %s jÃ¡ existe, broker %s cancela eleiÃ§Ã£o", ab.liderAtual, ab.idBroker)
		}
		return
	}

	// Envia mensagem de eleiÃ§Ã£o para brokers maiores
	ab.enviarMensagensEleicao(brokersMaiores)

	// Aguarda respostas
	select {
	case resposta := <-ab.canalEleicao:
		if resposta.Tipo == "RESPOSTA_ELEICAO" {
			utils.RegistrarLog("INFO", "Broker %s recebeu resposta de eleiÃ§Ã£o de %s",
				ab.idBroker, resposta.OrigemID)
			ab.aguardarVitoria()
		}
	case <-time.After(ab.tempoEsperaResposta):
		// Timeout, verifica se ainda não há líder antes de declarar vitória
		ab.mutex.RLock()
		liderExistente := ab.liderAtual
		ab.mutex.RUnlock()
		
		if liderExistente == "" {
			utils.RegistrarLog("INFO", "Broker %s timeout sem respostas, declarando vitÃ³ria", ab.idBroker)
			ab.declararVitoria()
		} else {
			ab.mutex.Lock()
			ab.emEleicao = false
			ab.mutex.Unlock()
			utils.RegistrarLog("INFO", "LÃ­der %s jÃ¡ existe durante timeout, broker %s cancela", liderExistente, ab.idBroker)
		}
	}
}

// aguardarVitoria aguarda a declaraÃ§Ã£o de vitÃ³ria de um broker maior
func (ab *AlgoritmoBully) aguardarVitoria() {
	utils.RegistrarLog("INFO", "Broker %s aguardando anÃºncio de vitÃ³ria", ab.idBroker)

	// Aguarda mensagem de VITORIA ou timeout
	select {
	case msg := <-ab.canalEleicao:
		if msg.Tipo == "VITORIA" {
			utils.RegistrarLog("INFO", "Broker %s recebeu anÃºncio de vitÃ³ria de %s",
				ab.idBroker, msg.OrigemID)
			ab.mutex.Lock()
			ab.emEleicao = false
			ab.mutex.Unlock()
		}
	case <-time.After(ab.tempoEsperaVitoria):
		// Timeout aguardando vitÃ³ria, inicia nova eleiÃ§Ã£o
		utils.RegistrarLog("AVISO", "Broker %s timeout aguardando vitÃ³ria, iniciando nova eleiÃ§Ã£o", ab.idBroker)
		ab.mutex.Lock()
		ab.emEleicao = false
		ab.mutex.Unlock()
		go ab.IniciarEleicao()
	}
}

// encontrarBrokersMaiores retorna lista de brokers com ID maior
func (ab *AlgoritmoBully) encontrarBrokersMaiores() []*tipos.Vizinho {
	var maiores []*tipos.Vizinho

	ab.mutex.RLock()
	defer ab.mutex.RUnlock()

	for _, vizinho := range ab.vizinhos {
		if vizinho.Ativo && compararPrioridadeID(vizinho.ID, ab.idBroker) > 0 {
			maiores = append(maiores, vizinho)
		}
	}

	return maiores
}

// enviarMensagensEleicao envia mensagens de eleiÃ§Ã£o para brokers maiores
func (ab *AlgoritmoBully) enviarMensagensEleicao(brokers []*tipos.Vizinho) {
	mensagem := tipos.Mensagem{
		Tipo:         "ELEICAO",
		OrigemID:     ab.idBroker,
		CarimboTempo: time.Now(),
	}

	for _, broker := range brokers {
		go func(c *tipos.Vizinho) {
			if err := ab.enviarMensagemTCP(c.EnderecoTCP, mensagem); err != nil {
				// Durante startup, "connection refused" Ã© esperado - outros brokers podem nÃ£o estar prontos
				// Log como DEBUG ao invÃ©s de ERRO para evitar poluiÃ§Ã£o de logs
				if isConnectionRefused(err) {
					utils.RegistrarLog("DEBUG", "Broker %s ainda nÃ£o estÃ¡ disponÃ­vel para eleiÃ§Ã£o: %s", c.ID, err)
				} else {
					utils.RegistrarLog("ERRO", "Falha ao enviar eleiÃ§Ã£o para %s: %v", c.ID, err)
				}
			}
		}(broker)
	}
}

// isConnectionRefused verifica se o erro Ã© "connection refused"
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "Connection refused")
}

// declararVitoria declara este broker como vencedor da eleiÃ§Ã£o
func (ab *AlgoritmoBully) declararVitoria() {
	ab.mutex.Lock()
	ab.liderAtual = ab.idBroker
	ab.emEleicao = false
	ab.mutex.Unlock()

	utils.RegistrarLog("INFO", "Broker %s se declarou lÃ­der", ab.idBroker)

	// Anuncia vitÃ³ria para todos os vizinhos
	ab.anunciarVitoria()

	// Notifica resultado
	select {
	case ab.canalResultado <- ab.idBroker:
	default:
	}
}

// anunciarVitoria anuncia vitÃ³ria para todos os vizinhos
func (ab *AlgoritmoBully) anunciarVitoria() {
	mensagem := tipos.Mensagem{
		Tipo:         "VITORIA",
		OrigemID:     ab.idBroker,
		Dados:        map[string]string{"lider": ab.idBroker},
		CarimboTempo: time.Now(),
	}

	ab.mutex.RLock()
	defer ab.mutex.RUnlock()

	for _, vizinho := range ab.vizinhos {
		if vizinho.Ativo {
			go func(v *tipos.Vizinho) {
				if err := ab.enviarMensagemTCP(v.EnderecoTCP, mensagem); err != nil {
					utils.RegistrarLog("ERRO", "Falha ao anunciar vitÃ³ria para %s: %v", v.ID, err)
				}
			}(vizinho)
		}
	}
}

// ProcessarMensagemEleicao processa mensagens relacionadas Ã  eleiÃ§Ã£o
func (ab *AlgoritmoBully) ProcessarMensagemEleicao(msg tipos.Mensagem) {
	switch msg.Tipo {
	case "ELEICAO":
		// Responde Ã  mensagem de eleiÃ§Ã£o
		resposta := tipos.Mensagem{
			Tipo:         "RESPOSTA_ELEICAO",
			OrigemID:     ab.idBroker,
			DestinoID:    msg.OrigemID,
			CarimboTempo: time.Now(),
		}

		ab.mutex.RLock()
		vizinho, existe := ab.vizinhos[msg.OrigemID]
		ab.mutex.RUnlock()

		if existe {
			ab.enviarMensagemTCP(vizinho.EnderecoTCP, resposta)
		}

		// Inicia prÃ³pria eleiÃ§Ã£o se nÃ£o estiver em uma
		ab.mutex.RLock()
		emEleicao := ab.emEleicao
		ab.mutex.RUnlock()

		if !emEleicao {
			go ab.IniciarEleicao()
		}

	case "RESPOSTA_ELEICAO":
		// Encaminha resposta para o canal
		select {
		case ab.canalEleicao <- msg:
		default:
			utils.RegistrarLog("AVISO", "Canal de eleiÃ§Ã£o cheio, descartando mensagem de %s", msg.OrigemID)
		}

	case "VITORIA":
		// Atualiza lÃ­der
		if dados, ok := msg.Dados.(map[string]interface{}); ok {
			if lider, existe := dados["lider"]; existe {
				liderStr, ok := lider.(string)
				if !ok || liderStr == "" {
					utils.RegistrarLog("AVISO", "Mensagem VITORIA invÃ¡lida recebida de %s", msg.OrigemID)
					return
				}

				ab.mutex.Lock()
				ab.liderAtual = liderStr
				ab.emEleicao = false
				ab.mutex.Unlock()

				utils.RegistrarLog("INFO", "Broker %s reconhece %s como lÃ­der",
					ab.idBroker, liderStr)

				// Encaminha para o canal de eleiÃ§Ã£o tambÃ©m
				select {
				case ab.canalEleicao <- msg:
				default:
				}

				select {
				case ab.canalResultado <- liderStr:
				default:
				}
			}
		}
	}
}

// enviarMensagemTCP envia uma mensagem via TCP
func (ab *AlgoritmoBully) enviarMensagemTCP(endereco string, msg tipos.Mensagem) error {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return err
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = conexao.Write(append(dados, '\n'))
	return err
}

// ObterLiderAtual retorna o lÃ­der atual
func (ab *AlgoritmoBully) ObterLiderAtual() string {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return ab.liderAtual
}

// ObterCanalResultado retorna o canal de resultados
func (ab *AlgoritmoBully) ObterCanalResultado() <-chan string {
	return ab.canalResultado
}

// EstaEmEleicao retorna se o broker estÃ¡ participando de uma eleiÃ§Ã£o
func (ab *AlgoritmoBully) EstaEmEleicao() bool {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return ab.emEleicao
}

// AtualizarVizinhos atualiza a lista de vizinhos
func (ab *AlgoritmoBully) AtualizarVizinhos(vizinhos map[string]*tipos.Vizinho) {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()
	ab.vizinhos = vizinhos
}

var sufixoNumericoID = regexp.MustCompile(`(\d+)$`)

// compararPrioridadeID compara IDs no formato "nome-<numero>".
// Retorna 1 se a > b, -1 se a < b e 0 se iguais.
func compararPrioridadeID(a, b string) int {
	na, oka := extrairNumeroID(a)
	nb, okb := extrairNumeroID(b)
	if oka && okb {
		switch {
		case na > nb:
			return 1
		case na < nb:
			return -1
		default:
			return 0
		}
	}

	// Fallback lexicogrÃ¡fico para IDs fora do padrÃ£o.
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}

func extrairNumeroID(id string) (int, bool) {
	match := sufixoNumericoID.FindStringSubmatch(id)
	if len(match) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

