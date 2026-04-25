package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"
)

type comandoDrone struct {
	Tipo           string    `json:"tipo"`
	DroneID        string    `json:"drone_id"`
	RequisicaoID   string    `json:"requisicao_id"`
	SetorID        string    `json:"setor_id"`
	CallbackBroker string    `json:"callback_broker"`
	CarimboTempo   time.Time `json:"carimbo_tempo"`
}

func main() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	droneID := flag.String("id", env("DRONE_ID", "drone-01"), "ID do drone")
	porta := flag.String("port", env("DRONE_PORT", ":9101"), "porta tcp")
	flag.Parse()

	prefixo := fmt.Sprintf("[DRONE-%s]", strings.ToUpper(strings.TrimPrefix(*droneID, "drone-")))
	listener, err := net.Listen("tcp", *porta)
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	fmt.Printf("%s iniciado em %s estado=DISPONIVEL\n", prefixo, *porta)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go func(c net.Conn) {
			defer c.Close()
			linha, err := bufio.NewReader(c).ReadBytes('\n')
			if err != nil {
				return
			}
			var cmd comandoDrone
			if err := json.Unmarshal(linha, &cmd); err != nil {
				return
			}
			if cmd.Tipo != "START_MISSION" || cmd.DroneID != *droneID {
				return
			}
			fmt.Printf("%s req=%s setor=%s estado=EM_MISSAO\n", prefixo, cmd.RequisicaoID, cmd.SetorID)
			time.Sleep(time.Duration(5+rng.Intn(10)) * time.Second)
			fmt.Printf("%s req=%s concluida estado=DISPONIVEL\n", prefixo, cmd.RequisicaoID)
			notificarDisponivel(*droneID, cmd.RequisicaoID, cmd.CallbackBroker)
		}(conn)
	}
}

func notificarDisponivel(droneID, reqID, callback string) {
	callback = strings.TrimSpace(callback)
	if callback == "" {
		return
	}
	conn, err := net.DialTimeout("tcp", callback, 3*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	msg := map[string]interface{}{
		"tipo":          "DRONE_DISPONIVEL",
		"origem_id":     droneID,
		"carimbo_tempo": time.Now(),
		"dados": map[string]string{
			"drone_id":      droneID,
			"requisicao_id": reqID,
			"estado":        "DISPONIVEL",
		},
	}
	dados, _ := json.Marshal(msg)
	_, _ = conn.Write(append(dados, '\n'))
}

func env(chave, padrao string) string {
	v := strings.TrimSpace(os.Getenv(chave))
	if v == "" {
		return padrao
	}
	return v
}
