package gossip

import (
	"encoding/json"
	"math/rand"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocoloGossip implementa o protocolo de gossip para disseminação de estado
type ProtocoloGossip struct {
	idBroker         string
	estado           *tipos.EstadoBroker
	vizinhos         map[string]*tipos.Vizinho
	intervaloGossip  time.Duration
	mutex            sync.RWMutex
	executando       atomic.Bool
	canalAtualizacao chan *tipos.EstadoBroker
	pararCh          chan struct{}
	pararOnce        sync.Once
}

// NovoProtocoloGossip cria uma nova instância do protocolo gossip
func NovoProtocoloGossip(idBroker string, estado *tipos.EstadoBroker,
	vizinhos map[string]*tipos.Vizinho) *ProtocoloGossip {

	return &ProtocoloGossip{
		idBroker:         idBroker,
		estado:           estado,
		vizinhos:         vizinhos,
		intervaloGossip:  5 * time.Second,
		canalAtualizacao: make(chan *tipos.EstadoBroker, 10),
		pararCh:          make(chan struct{}),
	}
}

// Iniciar inicia o protocolo gossip
func (pg *ProtocoloGossip) Iniciar() {
	pg.executando.Store(true)
	go pg.disseminarEstado()
	go pg.receberAtualizacoes()

	utils.RegistrarLog("INFO", "Protocolo gossip iniciado para broker %s", pg.idBroker)
}

// disseminarEstado dissemina periodicamente o estado para vizinhos aleatórios
func (pg *ProtocoloGossip) disseminarEstado() {
	ticker := time.NewTicker(pg.intervaloGossip)
	defer ticker.Stop()

	for pg.executando.Load() {
		select {
		case <-ticker.C:
		case <-pg.pararCh:
			return
		}

		pg.mutex.RLock()
		vizinhosAtivos := pg.obterVizinhosAtivos()
		estadoSnapshot := *pg.estado
		pg.mutex.RUnlock()

		if len(vizinhosAtivos) == 0 {
			continue
		}

		// Seleciona até 3 vizinhos aleatórios
		selecionados := pg.selecionarVizinhosAleatorios(vizinhosAtivos, 3)

		mensagem := tipos.Mensagem{
			Tipo:         "GOSSIP",
			OrigemID:     pg.idBroker,
			Dados:        estadoSnapshot,
			CarimboTempo: time.Now(),
		}

		for _, vizinho := range selecionados {
			go func(v *tipos.Vizinho) {
				pg.enviarEstadoUDP(v.EnderecoUDP, mensagem)
			}(vizinho)
		}
	}
}

// receberAtualizacoes processa atualizações de estado recebidas
func (pg *ProtocoloGossip) receberAtualizacoes() {
	for pg.executando.Load() {
		select {
		case novoEstado := <-pg.canalAtualizacao:
			pg.mesclarEstado(novoEstado)
		case <-pg.pararCh:
			return
		}
	}
}

// ProcessarMensagemGossip processa uma mensagem gossip recebida
func (pg *ProtocoloGossip) ProcessarMensagemGossip(msg tipos.Mensagem) {
	if msg.Tipo != "GOSSIP" {
		return
	}

	estadoRecebido, ok := pg.extrairEstado(msg.Dados)
	if !ok {
		return
	}

	pg.mutex.RLock()
	versaoAtual := pg.estado.Versao
	pg.mutex.RUnlock()

	if estadoRecebido.Versao > versaoAtual {
		select {
		case pg.canalAtualizacao <- estadoRecebido:
		default:
		}
	}
}

// mesclarEstado mescla um estado recebido com o estado local
func (pg *ProtocoloGossip) mesclarEstado(novoEstado *tipos.EstadoBroker) {
	pg.mutex.Lock()
	defer pg.mutex.Unlock()

	if novoEstado.Versao <= pg.estado.Versao {
		return
	}

	// Atualiza recursos
	for id, recurso := range novoEstado.Recursos {
		if recursoLocal, existe := pg.estado.Recursos[id]; !existe ||
			recurso.Versao > recursoLocal.Versao {
			pg.estado.Recursos[id] = recurso
		}
	}

	// Atualiza informações de vizinhos
	for id, vizinho := range novoEstado.Vizinhos {
		if id != pg.idBroker {
			if vizinhoLocal, existe := pg.estado.Vizinhos[id]; !existe ||
				vizinho.VersaoEstado > vizinhoLocal.VersaoEstado {
				pg.estado.Vizinhos[id] = vizinho
			}
		}
	}

	pg.estado.Versao = novoEstado.Versao
	pg.estado.UltimaAtualizacao = time.Now()

	utils.RegistrarLog("INFO", "Estado atualizado para versão %d", pg.estado.Versao)
}

// obterVizinhosAtivos retorna lista de vizinhos ativos
func (pg *ProtocoloGossip) obterVizinhosAtivos() []*tipos.Vizinho {
	var ativos []*tipos.Vizinho

	for _, vizinho := range pg.vizinhos {
		if vizinho.Ativo {
			ativos = append(ativos, vizinho)
		}
	}

	return ativos
}

// selecionarVizinhosAleatorios seleciona n vizinhos aleatórios
func (pg *ProtocoloGossip) selecionarVizinhosAleatorios(vizinhos []*tipos.Vizinho, n int) []*tipos.Vizinho {
	if len(vizinhos) <= n {
		return vizinhos
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	embaralhados := make([]*tipos.Vizinho, len(vizinhos))
	copy(embaralhados, vizinhos)
	rng.Shuffle(len(embaralhados), func(i, j int) {
		embaralhados[i], embaralhados[j] = embaralhados[j], embaralhados[i]
	})
	return embaralhados[:n]
}

// enviarEstadoUDP envia estado via UDP para um vizinho
func (pg *ProtocoloGossip) enviarEstadoUDP(endereco string, msg tipos.Mensagem) error {
	enderecoUDP, err := net.ResolveUDPAddr("udp", endereco)
	if err != nil {
		return err
	}

	conexao, err := net.DialUDP("udp", nil, enderecoUDP)
	if err != nil {
		return err
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = conexao.Write(dados)
	return err
}

// Parar interrompe o protocolo gossip
func (pg *ProtocoloGossip) Parar() {
	pg.executando.Store(false)
	pg.pararOnce.Do(func() {
		close(pg.pararCh)
	})
}

func (pg *ProtocoloGossip) extrairEstado(dados interface{}) (*tipos.EstadoBroker, bool) {
	switch v := dados.(type) {
	case *tipos.EstadoBroker:
		return v, true
	case tipos.EstadoBroker:
		estado := v
		return &estado, true
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var estado tipos.EstadoBroker
		if err := json.Unmarshal(bytes, &estado); err != nil {
			return nil, false
		}
		return &estado, true
	}
}
