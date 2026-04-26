package fila

import (
	"container/heap"
	"encoding/json"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// ItemFila representa um item na fila de prioridade
type ItemFila struct {
	Requisicao   *tipos.Requisicao
	Prioridade   int
	Indice       int
	CarimboTempo time.Time
}

// FilaPrioridade implementa heap.Interface para fila de prioridade
type FilaPrioridade []*ItemFila

func (fp FilaPrioridade) Len() int { return len(fp) }

func (fp FilaPrioridade) Less(i, j int) bool {
	// Critério 1: Prioridade (maior = mais importante)
	if fp[i].Prioridade != fp[j].Prioridade {
		return fp[i].Prioridade > fp[j].Prioridade
	}

	// Critério 2: Grau de Criticidade (maior = mais crítico)
	if fp[i].Requisicao.GrauCriticidade != fp[j].Requisicao.GrauCriticidade {
		return fp[i].Requisicao.GrauCriticidade > fp[j].Requisicao.GrauCriticidade
	}

	// Critério 3: Timestamp de entrada (mais antigo primeiro - FIFO)
	return fp[i].Requisicao.TimestampEntrada.Before(fp[j].Requisicao.TimestampEntrada)
}

func (fp FilaPrioridade) Swap(i, j int) {
	fp[i], fp[j] = fp[j], fp[i]
	fp[i].Indice = i
	fp[j].Indice = j
}

func (fp *FilaPrioridade) Push(x interface{}) {
	n := len(*fp)
	item := x.(*ItemFila)
	item.Indice = n
	*fp = append(*fp, item)
}

func (fp *FilaPrioridade) Pop() interface{} {
	old := *fp
	n := len(old)
	item := old[n-1]
	item.Indice = -1
	*fp = old[0 : n-1]
	return item
}

// SerializarFila serializa toda a fila para transferência
func (fd *FilaDistribuida) SerializarFila() ([]byte, error) {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	items := make([]*tipos.Requisicao, 0, fd.fila.Len())
	for _, item := range fd.fila {
		items = append(items, item.Requisicao)
	}
	return json.Marshal(items)
}

// DeserializarEAdicionarFila adiciona requisições serializadas à fila
func (fd *FilaDistribuida) DeserializarEAdicionarFila(dados []byte) error {
	var requisicoes []*tipos.Requisicao
	if err := json.Unmarshal(dados, &requisicoes); err != nil {
		return err
	}

	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	for _, req := range requisicoes {
		// Verifica se já não existe na fila
		if _, existe := fd.requisicoesPorID[req.ID]; existe {
			continue
		}

		// Garante timestamp de entrada
		if req.TimestampEntrada.IsZero() {
			req.TimestampEntrada = time.Now()
		}

		// Garante timeout padrão
		if req.Timeout == 0 {
			req.Timeout = 30 * time.Second
		}

		prioridadeFinal := fd.calcularPrioridadeFinal(req)

		item := &ItemFila{
			Requisicao:   req,
			Prioridade:   prioridadeFinal,
			CarimboTempo: time.Now(),
		}

		heap.Push(&fd.fila, item)
		fd.requisicoesPorID[req.ID] = item

		utils.RegistrarLog("INFO", "Requisicao %s (prioridade=%d) transferida para broker %s",
			req.ID, prioridadeFinal, fd.idBroker)
	}

	utils.RegistrarLog("INFO", "Transferidas %d requisicoes para broker %s",
		len(requisicoes), fd.idBroker)

	return nil
}

// ObterTodasRequisicoes retorna todas as requisições da fila
func (fd *FilaDistribuida) ObterTodasRequisicoes() []*tipos.Requisicao {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	resultado := make([]*tipos.Requisicao, 0, fd.fila.Len())
	for _, item := range fd.fila {
		resultado = append(resultado, item.Requisicao)
	}
	return resultado
}

// FilaDistribuida representa uma fila distribuída com prioridade
type FilaDistribuida struct {
	idBroker           string
	fila               FilaPrioridade
	requisicoesPorID   map[string]*ItemFila
	canalProcessamento chan *tipos.Requisicao
	executando         bool
	mutex              sync.RWMutex
	// Cache de últimas requisições processadas (para evitar duplicidade)
	ultimasProcessadas map[string]time.Time
	tempoCache         time.Duration
}

// NovaFilaDistribuida cria uma nova fila distribuída
func NovaFilaDistribuida(idBroker string) *FilaDistribuida {
	fd := &FilaDistribuida{
		idBroker:           idBroker,
		fila:               make(FilaPrioridade, 0),
		requisicoesPorID:   make(map[string]*ItemFila),
		canalProcessamento: make(chan *tipos.Requisicao, 100),
		executando:         true,
		ultimasProcessadas: make(map[string]time.Time),
		tempoCache:         5 * time.Minute,
	}
	heap.Init(&fd.fila)
	return fd
}

// AdicionarRequisicao adiciona uma requisicao à fila
func (fd *FilaDistribuida) AdicionarRequisicao(req *tipos.Requisicao) error {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if !fd.executando {
		return nil
	}

	// Verifica se já foi processada recentemente (cache)
	if ultimo, existe := fd.ultimasProcessadas[req.ID]; existe {
		if time.Since(ultimo) < fd.tempoCache {
			utils.RegistrarLog("INFO", "Requisicao %s ja foi processada recentemente, ignorando", req.ID)
			return nil
		}
	}

	// Verifica se já existe na fila
	if _, existe := fd.requisicoesPorID[req.ID]; existe {
		utils.RegistrarLog("AVISO", "Requisicao %s ja existe na fila, atualizando prioridade", req.ID)
		// Atualiza prioridade se for maior
		if item, ok := fd.requisicoesPorID[req.ID]; ok {
			if req.Prioridade > item.Prioridade {
				item.Prioridade = req.Prioridade
				item.Requisicao.Prioridade = req.Prioridade
				item.Requisicao.GrauCriticidade = req.GrauCriticidade
				heap.Fix(&fd.fila, item.Indice)
			}
		}
		return nil
	}

	// Define timestamp de entrada se não tiver
	if req.TimestampEntrada.IsZero() {
		req.TimestampEntrada = time.Now()
	}

	// Define timeout padrão se não tiver
	if req.Timeout == 0 {
		req.Timeout = 30 * time.Second
	}

	// Calcula prioridade final baseada em múltiplos fatores
	prioridadeFinal := fd.calcularPrioridadeFinal(req)

	item := &ItemFila{
		Requisicao:   req,
		Prioridade:   prioridadeFinal,
		CarimboTempo: time.Now(),
	}

	heap.Push(&fd.fila, item)
	fd.requisicoesPorID[req.ID] = item

	utils.RegistrarLog("INFO", "Requisição %s adicionada à fila do broker %s (prioridade=%d, criticidade=%d)",
		req.ID, fd.idBroker, prioridadeFinal, req.GrauCriticidade)

	// Notifica processamento
	fd.notificarProximaRequisicao()
	return nil
}

// calcularPrioridadeFinal calcula a prioridade baseada em múltiplos fatores
func (fd *FilaDistribuida) calcularPrioridadeFinal(req *tipos.Requisicao) int {
	prioridadeBase := req.Prioridade

	// Aumenta prioridade baseada no grau de criticidade
	criticidadeBonus := req.GrauCriticidade

	// Aumenta prioridade para requisições que já tentaram várias vezes
	tentativasBonus := req.Tentativas / 2
	if tentativasBonus > 3 {
		tentativasBonus = 3
	}

	// Prioridade final (máximo 10)
	prioridadeFinal := prioridadeBase + criticidadeBonus + tentativasBonus
	if prioridadeFinal > 10 {
		prioridadeFinal = 10
	}

	return prioridadeFinal
}

// RemoverRequisicao remove uma requisição da fila
func (fd *FilaDistribuida) RemoverRequisicao(idRequisicao string) bool {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if item, existe := fd.requisicoesPorID[idRequisicao]; existe {
		heap.Remove(&fd.fila, item.Indice)
		delete(fd.requisicoesPorID, idRequisicao)

		// Adiciona ao cache de processadas
		fd.ultimasProcessadas[idRequisicao] = time.Now()

		// Limpa cache antigo periodicamente (a cada 100 remoções)
		if len(fd.ultimasProcessadas) > 100 {
			fd.limparCacheAntigo()
		}

		return true
	}
	return false
}

// limparCacheAntigo remove entradas antigas do cache
func (fd *FilaDistribuida) limparCacheAntigo() {
	agora := time.Now()
	for id, ts := range fd.ultimasProcessadas {
		if agora.Sub(ts) > fd.tempoCache {
			delete(fd.ultimasProcessadas, id)
		}
	}
}

// ObterProximaRequisicao obtém a próxima requisição da fila (já ordenada por prioridade)
func (fd *FilaDistribuida) ObterProximaRequisicao() *tipos.Requisicao {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if fd.fila.Len() == 0 {
		return nil
	}

	// Verifica timeout da primeira requisição
	item := fd.fila[0]
	if time.Since(item.Requisicao.TimestampEntrada) > item.Requisicao.Timeout {
		// Timeout atingido, remove e retorna a próxima
		heap.Pop(&fd.fila)
		delete(fd.requisicoesPorID, item.Requisicao.ID)
		utils.RegistrarLog("AVISO", "Requisicao %s removida por timeout", item.Requisicao.ID)
		return fd.ObterProximaRequisicao()
	}

	// Pega o item do topo sem remover (peek)
	return fd.fila[0].Requisicao
}

// ObterProximaRequisicaoERemove obtém e remove a próxima requisição
func (fd *FilaDistribuida) ObterProximaRequisicaoERemove() *tipos.Requisicao {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if fd.fila.Len() == 0 {
		return nil
	}

	item := heap.Pop(&fd.fila).(*ItemFila)
	delete(fd.requisicoesPorID, item.Requisicao.ID)

	// Adiciona ao cache
	fd.ultimasProcessadas[item.Requisicao.ID] = time.Now()

	return item.Requisicao
}

// notificarProximaRequisicao notifica o canal de processamento
func (fd *FilaDistribuida) notificarProximaRequisicao() {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	if !fd.executando || fd.fila.Len() == 0 {
		return
	}

	// Obtém a próxima requisição (sem remover) e envia para processamento
	proxima := fd.fila[0].Requisicao
	select {
	case fd.canalProcessamento <- proxima:
		utils.RegistrarLog("DEBUG", "Notificada requisição %s para processamento", proxima.ID)
	default:
		// Canal cheio, será processado depois
	}
}

// ObterCanalProcessamento retorna o canal de processamento
func (fd *FilaDistribuida) ObterCanalProcessamento() <-chan *tipos.Requisicao {
	return fd.canalProcessamento
}

// Tamanho retorna o tamanho da fila
func (fd *FilaDistribuida) Tamanho() int {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()
	return fd.fila.Len()
}

// ListarRequisicoes retorna a lista de requisições na fila (ordenadas)
func (fd *FilaDistribuida) ListarRequisicoes() []*tipos.Requisicao {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	resultado := make([]*tipos.Requisicao, fd.fila.Len())
	for i, item := range fd.fila {
		resultado[i] = item.Requisicao
	}
	return resultado
}

// ReordenarFila reordena a fila (útil após atualizações de prioridade)
func (fd *FilaDistribuida) ReordenarFila() {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()
	heap.Init(&fd.fila)
	utils.RegistrarLog("INFO", "Fila reordenada no broker %s", fd.idBroker)
}

// Parar interrompe a fila distribuída
func (fd *FilaDistribuida) Parar() {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if !fd.executando {
		return
	}
	fd.executando = false
	close(fd.canalProcessamento)
	utils.RegistrarLog("INFO", "Fila distribuída do broker %s parada", fd.idBroker)
}
