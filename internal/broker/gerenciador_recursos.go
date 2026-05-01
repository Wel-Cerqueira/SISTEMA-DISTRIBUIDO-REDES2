package broker

import (
	"fmt"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"sync"
	"time"
)

// GerenciadorRecursos gerencia recursos do sistema
type GerenciadorRecursos struct {
	idBroker string
	recursos map[string]*tipos.Recurso
	estado   *GerenciadorEstado
	mutex    sync.RWMutex
}

// NovoGerenciadorRecursos cria um novo gerenciador de recursos
func NovoGerenciadorRecursos(idBroker string, estado *GerenciadorEstado, dronesConfig string) *GerenciadorRecursos {
	gr := &GerenciadorRecursos{
		idBroker: idBroker,
		recursos: make(map[string]*tipos.Recurso),
		estado:   estado,
	}

	gr.inicializarRecursos(dronesConfig)
	return gr
}

// inicializarRecursos inicializa recursos padrão
// ARQUIVO: internal/broker/gerenciador_recursos.go

func (gr *GerenciadorRecursos) inicializarRecursos(dronesConfig string) {
	// Primeiro tenta carregar do estado persistido
	estado := gr.estado.ObterEstado()
	if estado != nil && len(estado.Recursos) > 0 {
		for id, recurso := range estado.Recursos {
			r := recurso
			gr.recursos[id] = &r
		}
		utils.RegistrarLog("INFO", "Recursos carregados do estado persistido para broker %s", gr.idBroker)
		return
	}

	// Se dronesConfig foi fornecido (via -drones flag), usa apenas os drones configurados
	// Caso contrário, NÃO cria drones padrão — apenas o broker configurado para isso deve ter drones
	if strings.TrimSpace(dronesConfig) != "" {
		// Parse já feito em parseDrones, aqui apenas registra os que foram atribuídos a este broker
		drones := parseDronesComoRecursos(dronesConfig, gr.idBroker)
		for i := range drones {
			gr.recursos[drones[i].ID] = &drones[i]
			gr.estado.AtualizarRecurso(drones[i])
		}
		utils.RegistrarLog("INFO", "Inicializados %d drones via config para broker %s",
			len(drones), gr.idBroker)
		return
	}

	utils.RegistrarLog("AVISO", "Broker %s sem drones configurados (use -drones para atribuir)", gr.idBroker)
}

// parseDronesComoRecursos converte a string de config em recursos do tipo drone
// Formato: "drone-01=host:porta,drone-02=host:porta,..."
func parseDronesComoRecursos(raw, brokerID string) []tipos.Recurso {
	var recursos []tipos.Recurso
	partes := strings.Split(raw, ",")
	for _, p := range partes {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) != 2 {
			continue
		}
		droneID := kv[0]
		recursos = append(recursos, tipos.Recurso{
			ID:             droneID,
			Nome:           fmt.Sprintf("Drone %s", droneID),
			Tipo:           "drone",
			Estado:         "disponivel",
			BrokerAtual:    brokerID,
			BloqueadoPor:   "",
			DonoRequisicao: "",
			UltimoAcesso:   time.Now(),
			Versao:         1,
		})
	}
	return recursos
}

// VerificarDisponibilidadeGlobal verifica se um recurso está realmente disponível
// Considera estado local e se há lock ativo
func (gr *GerenciadorRecursos) VerificarDisponibilidadeGlobal(recursoID string) (bool, string) {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	recurso, ok := gr.recursos[recursoID]
	if !ok {
		return false, "recurso_nao_encontrado"
	}

	// Verifica estado do recurso
	if recurso.Estado != "disponivel" {
		return false, fmt.Sprintf("recurso_em_estado_%s", recurso.Estado)
	}

	// Verifica se há lock ativo de outro broker
	if recurso.BloqueadoPor != "" && recurso.BloqueadoPor != gr.idBroker {
		return false, fmt.Sprintf("recurso_bloqueado_por_%s", recurso.BloqueadoPor)
	}

	return true, "disponivel"
}

// TentarAlocarRecurso tenta alocar um recurso com verificação de duplicidade
// Retorna (sucesso, recurso, mensagem_erro)
func (gr *GerenciadorRecursos) TentarAlocarRecurso(recursoID, requisicaoID, brokerSolicitante string) (*tipos.Recurso, bool, string) {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	recurso, ok := gr.recursos[recursoID]
	if !ok {
		return nil, false, "recurso_nao_encontrado"
	}

	// Verificações críticas para evitar dupla alocação
	if recurso.Estado != "disponivel" {
		return nil, false, fmt.Sprintf("recurso_em_estado_%s", recurso.Estado)
	}

	if recurso.BloqueadoPor != "" && recurso.BloqueadoPor != brokerSolicitante {
		return nil, false, fmt.Sprintf("recurso_bloqueado_por_%s", recurso.BloqueadoPor)
	}

	if recurso.DonoRequisicao != "" && recurso.DonoRequisicao != requisicaoID {
		return nil, false, "recurso_já_alocado_para_outra_requisicao"
	}

	// Aloca o recurso
	recurso.Estado = "em_uso"
	recurso.BloqueadoPor = brokerSolicitante
	recurso.DonoRequisicao = requisicaoID
	recurso.UltimoAcesso = time.Now()
	recurso.Versao++

	gr.recursos[recursoID] = recurso
	gr.estado.AtualizarRecurso(*recurso)

	utils.RegistrarLog("INFO", "Recurso %s alocado para requisicao %s pelo broker %s",
		recursoID, requisicaoID, brokerSolicitante)

	return recurso, true, "sucesso"
}

// AlocarRecurso aloca um recurso (método legado, agora usa TentarAlocarRecurso)
func (gr *GerenciadorRecursos) AlocarRecurso(recursoID string) (*tipos.Recurso, error) {
	recurso, sucesso, msg := gr.TentarAlocarRecurso(recursoID, "", gr.idBroker)
	if !sucesso {
		return nil, fmt.Errorf(msg)
	}
	return recurso, nil
}

// LiberarRecurso libera um recurso
func (gr *GerenciadorRecursos) LiberarRecurso(recursoID string) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	recurso, ok := gr.recursos[recursoID]
	if !ok {
		return fmt.Errorf("recurso %s nao encontrado", recursoID)
	}

	recurso.Estado = "disponivel"
	recurso.BloqueadoPor = ""
	recurso.DonoRequisicao = ""
	recurso.UltimoAcesso = time.Now()
	recurso.Versao++

	gr.recursos[recursoID] = recurso
	gr.estado.AtualizarRecurso(*recurso)

	utils.RegistrarLog("INFO", "Recurso %s liberado pelo broker %s", recursoID, gr.idBroker)
	return nil
}

// ObterRecursosDisponiveis retorna lista de recursos disponíveis
func (gr *GerenciadorRecursos) ObterRecursosDisponiveis() []*tipos.Recurso {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	disponiveis := make([]*tipos.Recurso, 0)
	for _, recurso := range gr.recursos {
		if recurso.Estado == "disponivel" && recurso.BloqueadoPor == "" {
			disponiveis = append(disponiveis, recurso)
		}
	}
	return disponiveis
}

// ObterRecurso retorna um recurso pelo ID
func (gr *GerenciadorRecursos) ObterRecurso(recursoID string) (*tipos.Recurso, bool) {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	recurso, ok := gr.recursos[recursoID]
	return recurso, ok
}

// SincronizarRecursos sincroniza recursos a partir de um estado recebido
func (gr *GerenciadorRecursos) SincronizarRecursos(estado *tipos.EstadoBroker) {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	for id, recurso := range estado.Recursos {
		if recursoLocal, existe := gr.recursos[id]; existe {
			// Se o recurso está marcado como disponível remotamente mas localmente está alocado
			// e o broker que alocou caiu, libera automaticamente
			if recurso.Estado == "disponivel" && recursoLocal.Estado == "em_uso" {
				if recursoLocal.BloqueadoPor != "" && recursoLocal.BloqueadoPor != gr.idBroker {
					// Verificar se o broker dono ainda está ativo
					vizinhos := gr.estado.ObterVizinhosAtivos()
					if _, ativo := vizinhos[recursoLocal.BloqueadoPor]; !ativo {
						// Broker dono caiu, libera o recurso
						recursoLocal.Estado = "disponivel"
						recursoLocal.BloqueadoPor = ""
						recursoLocal.DonoRequisicao = ""
						utils.RegistrarLog("AVISO", "Recurso %s liberado automaticamente (broker %s falhou)",
							id, recursoLocal.BloqueadoPor)
					}
				}
			}
			if recurso.Versao > recursoLocal.Versao {
				*recursoLocal = recurso
			}
		} else {
			r := recurso
			gr.recursos[id] = &r
		}
	}
}

// ProximoDroneDisponivel retorna o próximo drone disponível
func (gr *GerenciadorRecursos) ProximoDroneDisponivel() string {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	for id, recurso := range gr.recursos {
		if recurso.Tipo == "drone" && recurso.Estado == "disponivel" && recurso.BloqueadoPor == "" {
			return id
		}
	}
	return ""
}
