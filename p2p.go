package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type P2PNode struct {
	Host   host.Host
	Engine *Engine
}

func NewP2PNode(ctx context.Context, port int, engine *Engine) (*P2PNode, error) {
	h, err := libp2p.New(libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)))
	if err != nil {
		return nil, err
	}

	node := &P2PNode{Host: h, Engine: engine}

	h.SetStreamHandler("/pep/1.0.0", node.handleIncomingStream)
	h.SetStreamHandler("/pep/sync/1.0.0", node.handleSyncStream)

	fmt.Printf("🌐 Nodo P2P avviato.\n   ID: %s\n   Addr: %s\n", h.ID(), h.Addrs()[0])
	return node, nil
}

func (n *P2PNode) handleIncomingStream(s network.Stream) {
	defer s.Close()
	decoder := json.NewDecoder(bufio.NewReader(s))
	var ev Event
	if err := decoder.Decode(&ev); err != nil {
		fmt.Printf("⚠️ Errore decodifica: %v\n", err)
		return
	}
	fmt.Printf("📥 Evento ricevuto: %s (ID: %s...)\n", ev.Type, ev.ID[:8])
	if err := n.Engine.ProcessEvent(ev); err != nil {
		fmt.Printf("❌ Rifiutato: %v\n", err)
	} else {
		fmt.Println("✅ Validato e aggiunto al DAG locale!")
	}
}

// processBatch applica un batch di eventi ordinandoli topologicamente e verificando l'applicazione
func (n *P2PNode) processBatch(events []Event) {
	sorted := topologicalSort(events)
	applied := 0
	
	for _, ev := range sorted {
		if n.Engine.HasEvent(ev.ID) {
			continue // Già presente
		}
		
		if err := n.Engine.ProcessEvent(ev); err != nil {
			fmt.Printf("⚠️ Errore applicazione evento %s (%s): %v\n", ev.Type, ev.ID[:8], err)
			continue
		}
		
		// Verifica reale: l'evento è entrato nel log o è finito in orphanPool?
		if n.Engine.HasEvent(ev.ID) {
			applied++
		} else {
			fmt.Printf("⏳ Evento %s (%s) rimane in attesa (genitore mancante)\n", ev.Type, ev.ID[:8])
		}
	}
	fmt.Printf("✅ Elaborati %d/%d eventi con successo.\n", applied, len(events))
}

func (n *P2PNode) handleSyncStream(s network.Stream) {
	defer s.Close()
	var peerLog []Event
	if err := json.NewDecoder(bufio.NewReader(s)).Decode(&peerLog); err != nil {
		fmt.Printf("⚠️ Errore ricezione sync: %v\n", err)
		return
	}

	fmt.Printf("🔄 Ricevuti %d eventi dal peer. Elaborazione...\n", len(peerLog))
	n.processBatch(peerLog)

	myLog := n.Engine.GetEventLog()
	if err := json.NewEncoder(s).Encode(myLog); err != nil {
		fmt.Printf("⚠️ Errore invio sync: %v\n", err)
	} else {
		fmt.Printf("✅ Inviati %d eventi al peer come risposta.\n", len(myLog))
	}
}

func (n *P2PNode) SyncWith(ctx context.Context, targetPeer peer.AddrInfo) error {
	fmt.Printf("🔄 Avvio sincronizzazione con %s...\n", targetPeer.ID)
	if err := n.Host.Connect(ctx, targetPeer); err != nil {
		return fmt.Errorf("connessione fallita: %w", err)
	}

	s, err := n.Host.NewStream(ctx, targetPeer.ID, "/pep/sync/1.0.0")
	if err != nil {
		return fmt.Errorf("stream fallito: %w", err)
	}
	defer s.Close()

	myLog := n.Engine.GetEventLog()
	if err := json.NewEncoder(s).Encode(myLog); err != nil {
		return fmt.Errorf("invio log fallito: %w", err)
	}

	var peerLog []Event
	if err := json.NewDecoder(bufio.NewReader(s)).Decode(&peerLog); err != nil {
		return fmt.Errorf("ricezione log fallita: %w", err)
	}

	fmt.Printf("📥 Ricevuti %d eventi dal peer. Elaborazione...\n", len(peerLog))
	n.processBatch(peerLog)

	fmt.Println("✅ Sincronizzazione completata con successo.")
	return nil
}

func topologicalSort(events []Event) []Event {
	eventMap := make(map[string]Event)
	for _, ev := range events {
		eventMap[ev.ID] = ev
	}

	inDegree := make(map[string]int)
	for _, ev := range events {
		inDegree[ev.ID] = 0
	}

	for _, ev := range events {
		if ev.ParentHash != "" {
			if _, exists := eventMap[ev.ParentHash]; exists {
				inDegree[ev.ID]++
			}
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []Event
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, eventMap[id])

		for _, ev := range events {
			if ev.ParentHash == id {
				inDegree[ev.ID]--
				if inDegree[ev.ID] == 0 {
					queue = append(queue, ev.ID)
				}
			}
		}
	}
	return sorted
}

func ParseMultiaddr(addr string) (multiaddr.Multiaddr, error) {
	return multiaddr.NewMultiaddr(addr)
}