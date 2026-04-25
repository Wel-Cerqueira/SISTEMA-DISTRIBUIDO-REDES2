package exclusao_mutua

import (
	"bufio"
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// MutexDistribuido implementa exclusÃ£o mÃºtua distribuÃ­da
type MutexDistribuido struct {
	idBroker       string
	vizinhos         map[string]*tipos.Vizinho
	recursoBloqueado map[string]bool
	filaEspera       map[string][]string
	mutex            sync.RWMutex
	tempoEsperaLock  time.Duration
}

// NovoMutexDistribuido cria um novo mutex distribuÃ­do
func NovoMutexDistribuido(idBroker string, vizinhos map[string]*tipos.Vizinho) *MutexDistribuido {
	return &MutexDistribuido{
		idBroker:       idBroker,
		vizinhos:         vizinhos,
		recursoBloqueado: make(map[string]bool),
		filaEspera:       make(map[string][]string),
		tempoEsperaLock:  10 * time.Second,
	}
}

// SolicitarAcesso solicita acesso exclusivo a um recurso
func (md *MutexDistribuido) SolicitarAcesso(recursoID string) (bool, error) {
	md.mutex.Lock()

	// Verifica se o recurso jÃ¡ estÃ¡ bloqueado
	if bloqueado, existe := md.recursoBloqueado[recursoID]; existe && bloqueado {
		md.mutex.Unlock()
		return false, nil
	}

	// Marca como bloqueado temporariamente
	md.recursoBloqueado[recursoID] = true
	md.mutex.Unlock()

	// Solicita permissÃ£o dos vizinhos
	aprovacoes, totalAtivos := md.solicitarPermissoes(recursoID)

	// Inclui o prÃ³prio broker para maioria simples do conjunto participante.
	totalParticipantes := totalAtivos + 1
	maioria := (totalParticipantes / 2) + 1
	recebidas := 1 // AprovaÃ§Ã£o local
	pendentes := totalAtivos
	timeout := time.After(md.tempoEsperaLock)

	for pendentes > 0 {
		select {
		case aprovada := <-aprovacoes:
			pendentes--
			if aprovada {
				recebidas++
				if recebidas >= maioria {
					utils.RegistrarLog("INFO", "Broker %s obteve lock para recurso %s",
						md.idBroker, recursoID)
					return true, nil
				}
			}
		case <-timeout:
			utils.RegistrarLog("AVISO", "Timeout ao solicitar lock para recurso %s", recursoID)
			md.LiberarAcesso(recursoID)
			return false, nil
		}
	}

	// Libera o recurso em caso de quÃ³rum insuficiente.
	md.LiberarAcesso(recursoID)
	return false, nil
}

// solicitarPermissoes envia solicitaÃ§Ãµes de permissÃ£o para vizinhos
func (md *MutexDistribuido) solicitarPermissoes(recursoID string) (<-chan bool, int) {
	aprovacoes := make(chan bool, len(md.vizinhos))

	mensagem := tipos.Mensagem{
		Tipo:         "SOLICITACAO_LOCK",
		OrigemID:     md.idBroker,
		Dados:        map[string]string{"recurso_id": recursoID},
		CarimboTempo: time.Now(),
	}

	totalAtivos := 0
	for _, vizinho := range md.vizinhos {
		if !vizinho.Ativo {
			continue
		}
		totalAtivos++

		go func(v *tipos.Vizinho) {
			aprovacoes <- md.enviarSolicitacaoTCP(v.EnderecoTCP, mensagem)
		}(vizinho)
	}

	return aprovacoes, totalAtivos
}

// enviarSolicitacaoTCP envia solicitaÃ§Ã£o via TCP
func (md *MutexDistribuido) enviarSolicitacaoTCP(endereco string, msg tipos.Mensagem) bool {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return false
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	if _, err = conexao.Write(append(dados, '\n')); err != nil {
		return false
	}

	_ = conexao.SetReadDeadline(time.Now().Add(3 * time.Second))
	linha, err := bufio.NewReader(conexao).ReadBytes('\n')
	if err != nil {
		return false
	}

	var resposta tipos.Resposta
	if err := json.Unmarshal(linha, &resposta); err != nil {
		return false
	}
	return resposta.Sucesso
}

// ProcessarSolicitacaoLock processa uma solicitaÃ§Ã£o de lock
func (md *MutexDistribuido) ProcessarSolicitacaoLock(msg tipos.Mensagem) bool {
	dados, ok := msg.Dados.(map[string]interface{})
	if !ok {
		return false
	}

	recursoID, existe := dados["recurso_id"].(string)
	if !existe {
		return false
	}

	md.mutex.Lock()
	defer md.mutex.Unlock()

	// Verifica se o recurso estÃ¡ livre
	if bloqueado, existe := md.recursoBloqueado[recursoID]; existe && bloqueado {
		// Adiciona Ã  fila de espera
		md.filaEspera[recursoID] = append(md.filaEspera[recursoID], msg.OrigemID)
		return false
	}

	// Reserva o recurso localmente ao conceder a solicitaÃ§Ã£o.
	md.recursoBloqueado[recursoID] = true
	return true
}

// LiberarAcesso libera o acesso a um recurso
func (md *MutexDistribuido) LiberarAcesso(recursoID string) {
	md.mutex.Lock()
	defer md.mutex.Unlock()

	delete(md.recursoBloqueado, recursoID)

	// Notifica prÃ³ximo da fila de espera
	if fila, existe := md.filaEspera[recursoID]; existe && len(fila) > 0 {
		proximo := fila[0]
		md.filaEspera[recursoID] = fila[1:]

		// Notifica o prÃ³ximo broker
		if vizinho, existe := md.vizinhos[proximo]; existe {
			go md.notificarLiberacao(vizinho.EnderecoTCP, recursoID)
		}
	}

	utils.RegistrarLog("INFO", "Broker %s liberou lock do recurso %s", md.idBroker, recursoID)
}

// notificarLiberacao notifica um broker sobre liberaÃ§Ã£o de recurso
func (md *MutexDistribuido) notificarLiberacao(endereco, recursoID string) {
	mensagem := tipos.Mensagem{
		Tipo:         "LIBERACAO_LOCK",
		OrigemID:     md.idBroker,
		Dados:        map[string]string{"recurso_id": recursoID},
		CarimboTempo: time.Now(),
	}

	md.enviarSolicitacaoTCP(endereco, mensagem)
}

