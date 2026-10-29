package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto" // <-- Questo era mancante
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type P2PNode struct {
	Host   host.Host
	Engine *Engine
}

func NewP2PNode(ctx context.Context, port int, dataDir string, engine *Engine) (*P2PNode, error) {
	keyFile := filepath.Join(dataDir, "peer.key")
	var privKey crypto.PrivKey

	// Controlla se esiste già una chiave salvata
	if _, err := os.Stat(keyFile); err == nil {
		// Carica la chiave esistente
		keyBytes, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("errore lettura chiave: %v", err)
		}
		privKey, err = crypto.UnmarshalPrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("errore unmarshal chiave: %v", err)
		}
		fmt.Println("🔑 Chiave P2P caricata dal disco (PeerID costante)")
	} else {
		// Genera una nuova chiave
		var err error
		privKey, _, err = crypto.GenerateKeyPair(crypto.Ed25519, -1)
		if err != nil {
			return nil, fmt.Errorf("errore generazione chiave: %v", err)
		}
		// Salva la chiave su disco
		keyBytes, err := crypto.MarshalPrivateKey(privKey)
		if err != nil {
			return nil, fmt.Errorf("errore marshal chiave: %v", err)
		}
		if err := os.WriteFile(keyFile, keyBytes, 0600); err != nil {
			return nil, fmt.Errorf("errore salvataggio chiave: %v", err)
		}
		fmt.Println("🔑 Nuova chiave P2P generata e salvata su disco")
	}

	// Crea l'host libp2p con la chiave
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)),
		libp2p.Identity(privKey),
	)
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

// processBatch delega l'elaborazione e il salvataggio in batch direttamente all'Engine
func (n *P2PNode) processBatch(events []Event) {
	n.Engine.processBatch(events)
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

	// 1. Calcola il grado in entrata considerando TUTTI i genitori (ParentHash + ParentRefs)
	// Usiamo una mappa per evitare di contare due volte lo stesso genitore se presente in entrambi i campi
	for _, ev := range events {
		parents := make(map[string]bool)
		if ev.ParentHash != "" {
			parents[ev.ParentHash] = true
		}
		for _, ref := range ev.ParentRefs {
			if ref != "" {
				parents[ref] = true
			}
		}

		for parentID := range parents {
			if _, exists := eventMap[parentID]; exists {
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

		// 2. Decrementa il grado in entrata per tutti gli eventi che hanno 'id' come genitore
		for _, ev := range events {
			isParent := false
			if ev.ParentHash == id {
				isParent = true
			} else {
				for _, ref := range ev.ParentRefs {
					if ref == id {
						isParent = true
						break
					}
				}
			}

			if isParent {
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