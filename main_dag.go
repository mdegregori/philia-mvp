//go:build ignore

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

func main() {
	ctx := context.Background()
	
	// ✅ Crea directory uniche per questo test specifico per garantire una "lavagna pulita"
	timestamp := time.Now().UnixNano()
	dirA := fmt.Sprintf("data_dag_A_%d", timestamp)
	dirB := fmt.Sprintf("data_dag_B_%d", timestamp)
	
	fmt.Println("🕸️ --- DIMOSTRAZIONE CICLO COMPLETO DAG PEP (PULITO) --- 🕸️")
	fmt.Printf("📁 Utilizzo directory isolate: %s e %s\n\n", dirA, dirB)

	// 1. Inizializza due Engine e due Nodi con le nuove directory
	engineA := NewEngine(NewStore(dirA))
	engineB := NewEngine(NewStore(dirB))
	
	nodeA, _ := NewP2PNode(ctx, 4001, engineA)
	defer nodeA.Host.Close()
	
	nodeB, _ := NewP2PNode(ctx, 4002, engineB)
	defer nodeB.Host.Close()

	// 2. Identità
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	idA := hex.EncodeToString(pubA)
	
	pubB, privB, _ := ed25519.GenerateKey(rand.Reader)
	idB := hex.EncodeToString(pubB)

	fmt.Println("--- FASE 1: Lavoro Offline Parallelo (Prenotazione + Regolamento) ---")
	
	// --- NODO A ---
	genA := NewEvent(GENESIS, "", idA, "", 1000, time.Now().UnixNano(), "Node A Funding")
	genA.Sign(privA)
	engineA.ProcessEvent(genA)
	
	payAtoB := NewEvent(PAYMENT, genA.ID, idA, idB, 200, time.Now().UnixNano(), "Service A")
	payAtoB.Sign(privA)
	engineA.ProcessEvent(payAtoB)

	settleA := NewEvent(SETTLE, payAtoB.ID, idA, idB, 200, time.Now().UnixNano(), payAtoB.ID)
	settleA.Sign(privA)
	engineA.ProcessEvent(settleA)
	
	fmt.Printf("✅ Nodo A ha creato 3 eventi. (Saldo A: Ledger=%d, Reserved=%d)\n", getLedger(engineA, idA), getReserved(engineA, idA))

	// --- NODO B ---
	genB := NewEvent(GENESIS, "", idB, "", 1000, time.Now().UnixNano(), "Node B Funding")
	genB.Sign(privB)
	engineB.ProcessEvent(genB)
	
	payBtoA := NewEvent(PAYMENT, genB.ID, idB, idA, 300, time.Now().UnixNano(), "Service B")
	payBtoA.Sign(privB)
	engineB.ProcessEvent(payBtoA)

	settleB := NewEvent(SETTLE, payBtoA.ID, idB, idA, 300, time.Now().UnixNano(), payBtoA.ID)
	settleB.Sign(privB)
	engineB.ProcessEvent(settleB)
	
	fmt.Printf("✅ Nodo B ha creato 3 eventi. (Saldo B: Ledger=%d, Reserved=%d)\n", getLedger(engineB, idB), getReserved(engineB, idB))

	fmt.Println("\n--- FASE 2: Sincronizzazione Bidirezionale (La Rete Torna) ---")
	
	maddrB, _ := ParseMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/4002"))
	peerInfoB := peer.AddrInfo{ID: nodeB.Host.ID(), Addrs: []multiaddr.Multiaddr{maddrB}}

	err := nodeA.SyncWith(ctx, peerInfoB)
	if err != nil {
		fmt.Printf("❌ Errore sync: %v\n", err)
		return
	}

	fmt.Println("\n--- FASE 3: Verifica della Convergenza del DAG ---")
	
	logA := engineA.GetEventLog()
	logB := engineB.GetEventLog()
	
	fmt.Printf("📊 Nodo A: %d eventi nel DAG. Saldo A: Ledger=%d | Saldo B: Ledger=%d\n", 
		len(logA), getLedger(engineA, idA), getLedger(engineA, idB))
	fmt.Printf("📊 Nodo B: %d eventi nel DAG. Saldo A: Ledger=%d | Saldo B: Ledger=%d\n", 
		len(logB), getLedger(engineB, idA), getLedger(engineB, idB))

	// Verifica matematica: 
	// A inizia con 1000, paga 200, riceve 300 -> 1000 - 200 + 300 = 1100
	// B inizia con 1000, paga 300, riceve 200 -> 1000 - 300 + 200 = 900
	if len(logA) == 6 && len(logB) == 6 && getLedger(engineA, idA) == 1100 && getLedger(engineB, idB) == 900 {
		fmt.Println("\n🏆 SUCCESSO DAG COMPLETO: Entrambi i nodi hanno converguto sullo stato globale definitivo!")
		fmt.Println("   La 'Partita Prenotata' è stata regolata con successo attraverso la rete P2P.")
	} else {
		fmt.Println("\n⚠️ La convergenza o i calcoli non corrispondono alle aspettative.")
	}
}

func getLedger(e *Engine, id string) int64 {
	ledger, _ := e.GetBalance(id)
	return ledger
}

func getReserved(e *Engine, id string) int64 {
	_, reserved := e.GetBalance(id)
	return reserved
}