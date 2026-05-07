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
	Requisicao   *tipos.Requisicao // Ponteiro para a requisição
	Prioridade   int               // Prioridade calculada final
	Indice       int               // Índice no heap (para operações O(log n))
	CarimboTempo time.Time         // Timestamp de entrada na fila
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

// FilaPrioridade é uma struct que implementa a interface heap.Interface
// do Go para criar uma fila de prioridade eficiente:
type FilaPrioridade []*ItemFila

// Retorna o número de itens na fila
func (fp FilaPrioridade) Len() int {
	return len(fp)
}

// Algoritmo de Ordenação (3 níveis):
// Prioridade: Requisições com prioridade maior vêm primeiro
// Grau de Criticidade: Se prioridades iguais, mais crítico primeiro
// FIFO: Se ambos iguais, mais antigo primeiro (First In, First Out)
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

// AdicionarRequisicao adiciona uma requisição à fila.
// Chama notificarProximaRequisicaoSemLock internamente para evitar deadlock.
func (fd *FilaDistribuida) AdicionarRequisicao(req *tipos.Requisicao) error {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if !fd.executando {
		return nil
	}

	// Verifica se já foi processada recentemente (cache anti-duplicidade)
	if ultimo, existe := fd.ultimasProcessadas[req.ID]; existe {
		if time.Since(ultimo) < fd.tempoCache {
			utils.RegistrarLog("INFO", "Requisicao %s ja foi processada recentemente, ignorando", req.ID)
			return nil
		}
	}

	// Se já existe na fila, apenas atualiza a prioridade se for maior
	if item, existe := fd.requisicoesPorID[req.ID]; existe {
		utils.RegistrarLog("AVISO", "Requisicao %s ja existe na fila, atualizando prioridade", req.ID)
		if req.Prioridade > item.Prioridade {
			item.Prioridade = req.Prioridade
			item.Requisicao.Prioridade = req.Prioridade
			item.Requisicao.GrauCriticidade = req.GrauCriticidade
			heap.Fix(&fd.fila, item.Indice)
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

	// Notifica usando a versão sem lock (já estamos dentro do Lock acima)
	fd.notificarProximaRequisicaoSemLock()
	return nil
}

// notificarProximaRequisicaoSemLock envia a próxima requisição ao canal de processamento.
// DEVE ser chamada somente quando o mutex já está segurado pelo chamador.
func (fd *FilaDistribuida) notificarProximaRequisicaoSemLock() {
	if !fd.executando || fd.fila.Len() == 0 {
		return
	}
	proxima := fd.fila[0].Requisicao
	select {
	case fd.canalProcessamento <- proxima:
		utils.RegistrarLog("DEBUG", "Notificada requisição %s para processamento", proxima.ID)
	default:
		utils.RegistrarLog("AVISO", "Canal de processamento cheio - requisição %s aguardará (fila: %d)",
			proxima.ID, fd.fila.Len())
	}
}

// notificarProximaRequisicao é a versão pública que adquire seu próprio RLock.
// Mantida para compatibilidade caso seja chamada de fora do pacote.
func (fd *FilaDistribuida) notificarProximaRequisicao() {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()
	fd.notificarProximaRequisicaoSemLock()
}

// calcularPrioridadeFinal calcula a prioridade final baseada em múltiplos fatores
func (fd *FilaDistribuida) calcularPrioridadeFinal(req *tipos.Requisicao) int {
	prioridadeBase := req.Prioridade
	criticidadeBonus := req.GrauCriticidade

	// Bonus para requisições que já tentaram várias vezes (máx +3)
	tentativasBonus := req.Tentativas / 2
	if tentativasBonus > 3 {
		tentativasBonus = 3
	}

	// Prioridade final com teto em 10
	prioridadeFinal := prioridadeBase + criticidadeBonus + tentativasBonus
	if prioridadeFinal > 10 {
		prioridadeFinal = 10
	}
	return prioridadeFinal
}

// RemoverRequisicao remove uma requisição da fila pelo ID
func (fd *FilaDistribuida) RemoverRequisicao(idRequisicao string) bool {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if item, existe := fd.requisicoesPorID[idRequisicao]; existe {
		heap.Remove(&fd.fila, item.Indice)
		delete(fd.requisicoesPorID, idRequisicao)

		// Registra no cache para evitar reprocessamento
		fd.ultimasProcessadas[idRequisicao] = time.Now()

		// Limpa cache antigo a cada 100 entradas
		if len(fd.ultimasProcessadas) > 100 {
			fd.limparCacheAntigo()
		}
		return true
	}
	return false
}

// limparCacheAntigo remove entradas expiradas do cache de processadas
func (fd *FilaDistribuida) limparCacheAntigo() {
	agora := time.Now()
	for id, ts := range fd.ultimasProcessadas {
		if agora.Sub(ts) > fd.tempoCache {
			delete(fd.ultimasProcessadas, id)
		}
	}
}

// ObterProximaRequisicao retorna a próxima requisição sem removê-la da fila.
// Remove automaticamente requisições expiradas por timeout.
// Corrigido: usa loop em vez de recursão para evitar deadlock com lock segurado.
func (fd *FilaDistribuida) ObterProximaRequisicao() *tipos.Requisicao {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	for fd.fila.Len() > 0 {
		item := fd.fila[0]
		if time.Since(item.Requisicao.TimestampEntrada) > item.Requisicao.Timeout {
			heap.Pop(&fd.fila)
			delete(fd.requisicoesPorID, item.Requisicao.ID)
			fd.ultimasProcessadas[item.Requisicao.ID] = time.Now()
			utils.RegistrarLog("AVISO", "Requisicao %s removida por timeout", item.Requisicao.ID)
			continue
		}
		return item.Requisicao
	}
	return nil
}

// ObterPorID retorna uma requisição pelo ID sem removê-la, ou nil se não existir.
// Usado pelo processador para verificar se a requisição ainda está na fila.
func (fd *FilaDistribuida) ObterPorID(id string) *tipos.Requisicao {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()
	if item, existe := fd.requisicoesPorID[id]; existe {
		return item.Requisicao
	}
	return nil
}

// ObterProximaRequisicaoERemove obtém e remove atomicamente a próxima requisição
func (fd *FilaDistribuida) ObterProximaRequisicaoERemove() *tipos.Requisicao {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if fd.fila.Len() == 0 {
		return nil
	}

	item := heap.Pop(&fd.fila).(*ItemFila)
	delete(fd.requisicoesPorID, item.Requisicao.ID)
	fd.ultimasProcessadas[item.Requisicao.ID] = time.Now()
	return item.Requisicao
}

// RenotificarRequisicao coloca novamente uma requisição no canal de processamento.
// Usado após back-off quando uma tentativa anterior falhou.
func (fd *FilaDistribuida) RenotificarRequisicao(id string) {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	item, existe := fd.requisicoesPorID[id]
	if !existe || !fd.executando {
		return
	}
	select {
	case fd.canalProcessamento <- item.Requisicao:
		utils.RegistrarLog("DEBUG", "Requisição %s renotificada para processamento", id)
	default:
		// Canal cheio; será renotificada na próxima reordenação
	}
}

// SerializarFila serializa toda a fila atual para transferência entre brokers
func (fd *FilaDistribuida) SerializarFila() ([]byte, error) {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	items := make([]*tipos.Requisicao, 0, fd.fila.Len())
	for _, item := range fd.fila {
		items = append(items, item.Requisicao)
	}
	return json.Marshal(items)
}

// DeserializarEAdicionarFila adiciona requisições serializadas recebidas de outro broker.
// Ignora requisições que já existem na fila ou foram processadas recentemente.
func (fd *FilaDistribuida) DeserializarEAdicionarFila(dados []byte) error {
	var requisicoes []*tipos.Requisicao
	if err := json.Unmarshal(dados, &requisicoes); err != nil {
		return err
	}

	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	adicionadas := 0
	for _, req := range requisicoes {
		// Pula se já existe na fila
		if _, existe := fd.requisicoesPorID[req.ID]; existe {
			continue
		}
		// Pula se já foi processada recentemente
		if ultimo, existe := fd.ultimasProcessadas[req.ID]; existe {
			if time.Since(ultimo) < fd.tempoCache {
				continue
			}
		}

		if req.TimestampEntrada.IsZero() {
			req.TimestampEntrada = time.Now()
		}
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
		adicionadas++

		utils.RegistrarLog("INFO", "Requisicao %s (prioridade=%d) transferida para broker %s",
			req.ID, prioridadeFinal, fd.idBroker)
	}

	utils.RegistrarLog("INFO", "%d/%d requisicoes transferidas para broker %s",
		adicionadas, len(requisicoes), fd.idBroker)
	return nil
}

// ObterTodasRequisicoes retorna todas as requisições da fila sem removê-las
func (fd *FilaDistribuida) ObterTodasRequisicoes() []*tipos.Requisicao {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	resultado := make([]*tipos.Requisicao, 0, fd.fila.Len())
	for _, item := range fd.fila {
		resultado = append(resultado, item.Requisicao)
	}
	return resultado
}

// ObterCanalProcessamento retorna o canal de processamento (somente leitura)
func (fd *FilaDistribuida) ObterCanalProcessamento() <-chan *tipos.Requisicao {
	return fd.canalProcessamento
}

// Tamanho retorna a quantidade de requisições atualmente na fila
func (fd *FilaDistribuida) Tamanho() int {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()
	return fd.fila.Len()
}

// ListarRequisicoes retorna a lista de requisições na ordem atual do heap
func (fd *FilaDistribuida) ListarRequisicoes() []*tipos.Requisicao {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	resultado := make([]*tipos.Requisicao, fd.fila.Len())
	for i, item := range fd.fila {
		resultado[i] = item.Requisicao
	}
	return resultado
}

// ReordenarFila reconstrói o heap (útil após atualizações de prioridade em lote)
func (fd *FilaDistribuida) ReordenarFila() {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()
	heap.Init(&fd.fila)
	utils.RegistrarLog("INFO", "Fila reordenada no broker %s", fd.idBroker)
}

// Parar encerra a fila distribuída e fecha o canal de processamento
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

// Troca dois elementos e atualiza seus índices
func (fp FilaPrioridade) Swap(i, j int) {
	fp[i], fp[j] = fp[j], fp[i]
	fp[i].Indice = i
	fp[j].Indice = j
}

// Insere no heap
func (fp *FilaPrioridade) Push(x interface{}) {
	n := len(*fp)
	item := x.(*ItemFila)
	item.Indice = n
	*fp = append(*fp, item)
}

// remove do heap
func (fp *FilaPrioridade) Pop() interface{} {
	old := *fp
	n := len(old)
	item := old[n-1]
	item.Indice = -1
	*fp = old[0 : n-1]
	return item
}
