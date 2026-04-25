package exclusao_mutua

import (
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// MutexDistribuido implementa exclusão mútua distribuída
type MutexDistribuido struct {
	idBroker            string
	vizinhos            map[string]*tipos.Vizinho
	gerenciadorRecursos interface {
		VerificarDisponibilidadeGlobal(recursoID string) (bool, string)
		TentarAlocarRecurso(recursoID, requisicaoID, brokerSolicitante string) (*tipos.Recurso, bool, string)
		LiberarRecurso(recursoID string) error
	}
	recursoBloqueado map[string]bool
	filaEspera       map[string][]string // recursoID -> lista de requisicaoID
	mutex            sync.RWMutex
	tempoEsperaLock  time.Duration
}

// RecursoManager interface para o gerenciador de recursos
type RecursoManager interface {
	VerificarDisponibilidadeGlobal(recursoID string) (bool, string)
	TentarAlocarRecurso(recursoID, requisicaoID, brokerSolicitante string) (*tipos.Recurso, bool, string)
	LiberarRecurso(recursoID string) error
}

// NovoMutexDistribuido cria um novo mutex distribuído
func NovoMutexDistribuido(idBroker string, vizinhos map[string]*tipos.Vizinho) *MutexDistribuido {
	return &MutexDistribuido{
		idBroker:         idBroker,
		vizinhos:         vizinhos,
		recursoBloqueado: make(map[string]bool),
		filaEspera:       make(map[string][]string),
		tempoEsperaLock:  10 * time.Second,
	}
}

// SetRecursoManager configura o gerenciador de recursos
func (md *MutexDistribuido) SetRecursoManager(rm interface {
	VerificarDisponibilidadeGlobal(recursoID string) (bool, string)
	TentarAlocarRecurso(recursoID, requisicaoID, brokerSolicitante string) (*tipos.Recurso, bool, string)
	LiberarRecurso(recursoID string) error
}) {
	md.gerenciadorRecursos = rm
}

// SolicitarAcesso solicita acesso exclusivo a um recurso
// Agora verifica o estado global do recurso antes de conceder o lock
func (md *MutexDistribuido) SolicitarAcesso(recursoID, requisicaoID string) (bool, error) {
	// Primeiro, verifica disponibilidade global do recurso
	if md.gerenciadorRecursos != nil {
		disponivel, motivo := md.gerenciadorRecursos.VerificarDisponibilidadeGlobal(recursoID)
		if !disponivel {
			utils.RegistrarLog("AVISO", "Broker %s não pode solicitar lock para %s: %s",
				md.idBroker, recursoID, motivo)
			return false, nil
		}
	}

	md.mutex.Lock()
	// Verifica se o recurso já está bloqueado localmente
	if bloqueado, existe := md.recursoBloqueado[recursoID]; existe && bloqueado {
		// Adiciona à fila de espera
		md.filaEspera[recursoID] = append(md.filaEspera[recursoID], requisicaoID)
		md.mutex.Unlock()
		utils.RegistrarLog("INFO", "Requisicao %s em espera para recurso %s", requisicaoID, recursoID)
		return false, nil
	}

	// Marca como bloqueado temporariamente
	md.recursoBloqueado[recursoID] = true
	md.mutex.Unlock()

	// Solicita permissão dos vizinhos
	aprovacoes, totalAtivos := md.solicitarPermissoes(recursoID)

	// Inclui o próprio broker para maioria simples
	totalParticipantes := totalAtivos + 1
	maioria := (totalParticipantes / 2) + 1
	recebidas := 1 // Aprovação local
	pendentes := totalAtivos

	timeout := time.After(md.tempoEsperaLock)

	for pendentes > 0 {
		select {
		case aprovada := <-aprovacoes:
			pendentes--
			if aprovada {
				recebidas++
				if recebidas >= maioria {
					// Tentar alocar o recurso de fato
					if md.gerenciadorRecursos != nil {
						_, sucesso, motivo := md.gerenciadorRecursos.TentarAlocarRecurso(
							recursoID, requisicaoID, md.idBroker)
						if !sucesso {
							utils.RegistrarLog("ERRO", "Falha ao alocar recurso %s: %s", recursoID, motivo)
							md.LiberarAcesso(recursoID)
							return false, nil
						}
					}
					utils.RegistrarLog("INFO", "Broker %s obteve lock para recurso %s (requisicao %s)",
						md.idBroker, recursoID, requisicaoID)
					return true, nil
				}
			}
		case <-timeout:
			utils.RegistrarLog("AVISO", "Timeout ao solicitar lock para recurso %s", recursoID)
			md.LiberarAcesso(recursoID)
			return false, nil
		}
	}

	// Quórum insuficiente
	md.LiberarAcesso(recursoID)
	return false, nil
}

// solicitarPermissoes envia solicitações de permissão para vizinhos
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

// enviarSolicitacaoTCP envia solicitação TCP e aguarda resposta
func (md *MutexDistribuido) enviarSolicitacaoTCP(endereco string, msg tipos.Mensagem) bool {
	conn, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	_, err = conn.Write(append(dados, '\n'))
	if err != nil {
		return false
	}

	// Aguarda resposta
	var resposta tipos.Resposta
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resposta); err != nil {
		return false
	}
	return resposta.Sucesso
}

// ProcessarSolicitacaoLock processa uma solicitação de lock recebida
func (md *MutexDistribuido) ProcessarSolicitacaoLock(mensagem tipos.Mensagem) bool {
	recursoID, ok := mensagem.Dados.(map[string]interface{})["recurso_id"].(string)
	if !ok {
		return false
	}

	md.mutex.Lock()
	bloqueado, existe := md.recursoBloqueado[recursoID]
	md.mutex.Unlock()

	if existe && bloqueado {
		return false
	}

	// Verifica disponibilidade global
	if md.gerenciadorRecursos != nil {
		disponivel, _ := md.gerenciadorRecursos.VerificarDisponibilidadeGlobal(recursoID)
		if !disponivel {
			return false
		}
	}

	// Concede lock temporário
	md.mutex.Lock()
	md.recursoBloqueado[recursoID] = true
	md.mutex.Unlock()

	return true
}

// LiberarAcesso libera o acesso a um recurso
func (md *MutexDistribuido) LiberarAcesso(recursoID string) {
	md.mutex.Lock()
	defer md.mutex.Unlock()

	delete(md.recursoBloqueado, recursoID)

	// Libera o recurso no gerenciador
	if md.gerenciadorRecursos != nil {
		md.gerenciadorRecursos.LiberarRecurso(recursoID)
	}

	utils.RegistrarLog("INFO", "Broker %s liberou lock para recurso %s", md.idBroker, recursoID)

	// Notifica próximo da fila se houver
	if fila, ok := md.filaEspera[recursoID]; ok && len(fila) > 0 {
		// Remove o primeiro da fila
		md.filaEspera[recursoID] = fila[1:]
		utils.RegistrarLog("INFO", "Recurso %s liberado, próximo da fila pode tentar", recursoID)
	}
}

// AtualizarVizinhos atualiza a lista de vizinhos
func (md *MutexDistribuido) AtualizarVizinhos(vizinhos map[string]*tipos.Vizinho) {
	md.mutex.Lock()
	defer md.mutex.Unlock()
	md.vizinhos = vizinhos
}
