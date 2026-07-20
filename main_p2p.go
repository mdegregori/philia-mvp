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

	fmt.Println("🚀 --- DIMOSTRAZIONE RETE P2P PEP --- 🚀")

	// 1. Inizializza due Engine separati (con storage separato per chiarezza)
	engineA := NewEngine(NewStore("data_nodeA"))
	engineB := NewEngine(NewStore("data_nodeB"))

	// 2. Crea un'identità crittografica per il Nodo A (l'unico che genera eventi)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	idA := hex.EncodeToString(pubA)

	// 3. Avvia i due Nodi P2P su porte diverse
	nodeA, err := NewP2PNode(ctx, 4001, engineA)
	if err != nil {
		panic(err)
	}
	defer nodeA.Host.Close()

	nodeB, err := NewP2PNode(ctx, 4002, engineB)
	if err != nil {
		panic(err)
	}
	defer nodeB.Host.Close()

	fmt.Println("\n--- FASE 1: Il Nodo A crea un evento GENESIS ---")
	gen := NewEvent(GENESIS, "", idA, "", 1000, time.Now().UnixNano(), "P2P Test")
	gen.Sign(privA)

	// Il Nodo A lo processa localmente
	if err := engineA.ProcessEvent(gen); err != nil {
		fmt.Printf("❌ Errore locale Nodo A: %v\n", err)
		return
	}
	fmt.Println("✅ GENESIS processato localmente dal Nodo A.")

	fmt.Println("\n--- FASE 2: Il Nodo A invia l'evento al Nodo B via rete ---")
		// Costruiamo manualmente l'indirizzo del Nodo B
	maddrB, err := ParseMultiaddr("/ip4/127.0.0.1/tcp/4002")
	if err != nil {
		fmt.Printf("❌ Errore parsing indirizzo: %v\n", err)
		return
	}
	
	// Costruiamo manualmente il peer.AddrInfo
	peerInfoB := peer.AddrInfo{
		ID:    nodeB.Host.ID(),
		Addrs: []multiaddr.Multiaddr{maddrB},
	}

		// Inviamo l'evento!
	err = nodeA.SendEvent(ctx, peerInfoB, gen)
	if err != nil {
		fmt.Printf("❌ Errore invio: %v\n", err)
		return
	}

	// Diamo un piccolo delay per assicurare che la stampa del ricevitore appaia dopo
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\n--- FASE 3: Verifica dello stato ---")
	balanceA, _ := engineA.GetBalance(idA)
	balanceB, _ := engineB.GetBalance(idA)

	fmt.Printf("Saldo di %s... sul Nodo A (Mittente): %d\n", idA[:8], balanceA)
	fmt.Printf("Saldo di %s... sul Nodo B (Ricevente): %d\n", idA[:8], balanceB)

	if balanceA == 1000 && balanceB == 1000 {
		fmt.Println("\n🏆 SUCCESSO P2P: Il ledger si è sincronizzato correttamente tramite la rete!")
	} else {
		fmt.Println("\n⚠️ Qualcosa è andato storto nella sincronizzazione.")
	}
}