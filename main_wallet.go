//go:build ignore

package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var sessionKeys = make(map[string]ed25519.PrivateKey)
var currentUserIdentity string // Tiene traccia di chi sta usando la CLI

func getOrCreateKey(name string) (ed25519.PrivateKey, string) {
	if priv, exists := sessionKeys[name]; exists {
		pub := priv.Public().(ed25519.PublicKey)
		return priv, hex.EncodeToString(pub)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sessionKeys[name] = priv
	return priv, hex.EncodeToString(pub)
}

func main() {
	fmt.Println("==========================================")
	fmt.Println("       PHILIA ECONOMIC PROTOCOL (PEP)     ")
	fmt.Println("          Universal Wallet CLI            ")
	fmt.Println("==========================================")
	fmt.Println("Comandi:")
	fmt.Println("  create <nome>                 : Crea/Carica la tua identità")
	fmt.Println("  fund <importo>                : Ricevi fondi iniziali (GENESIS)")
	fmt.Println("  balance                       : Mostra i tuoi saldi")
	fmt.Println("  send <ID_dest> <importo> <memo>: Prenota un pagamento (PAYMENT)")
	fmt.Println("  settle <ID_dest> <importo>    : Regola definitivamente (SETTLE)")
	fmt.Println("  anchor                        : Notarizza il registro su blockchain")
	fmt.Println("  verify                        : Verifica l'integrità del registro")
	fmt.Println("  exit                          : Salva e chiudi")
	fmt.Println("==========================================")

	store := NewStore("data")
	engine := NewEngine(store)
	defer engine.Close()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		prompt := "pep> "
		if currentUserIdentity != "" {
			prompt = fmt.Sprintf("pep (%s)> ", currentUserIdentity)
		}
		fmt.Print(prompt)
		
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.Fields(input)
		cmd := strings.ToLower(parts[0])

		switch cmd {
		case "create":
			if len(parts) != 2 {
				fmt.Println("Uso: create <nome_utente>")
				continue
			}
			name := parts[1]
			_, id := getOrCreateKey(name)
			currentUserIdentity = name
			fmt.Printf("✅ Identità caricata: %s\n", name)
			fmt.Printf("   Il tuo ID Pubblico: %s...\n", id[:16])

		case "fund":
			if currentUserIdentity == "" {
				fmt.Println("❌ Devi prima creare un'identità con 'create <nome>'.")
				continue
			}
			if len(parts) != 2 {
				fmt.Println("Uso: fund <importo>")
				continue
			}
			amount := parseInt(parts[1])
			if amount <= 0 {
				fmt.Println("L'importo deve essere positivo.")
				continue
			}
			priv, id := getOrCreateKey(currentUserIdentity)
			ev := NewEvent(GENESIS, "", id, "", amount, time.Now().UnixNano(), "Wallet Funding")
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("❌ Errore: %v\n", err)
			} else {
				fmt.Printf("✅ Ricevuti %d Philia (GENESIS)\n", amount)
			}

		case "balance":
			if currentUserIdentity == "" {
				fmt.Println("❌ Identità non caricata.")
				continue
			}
			_, id := getOrCreateKey(currentUserIdentity)
			ledger, reserved := engine.GetBalance(id)
			fmt.Println("\n--- IL TUO SALDO ---")
			fmt.Printf("Totale (Ledger)   : %d Philia\n", ledger)
			fmt.Printf("In prenotazione   : %d Philia\n", reserved)
			fmt.Printf("Disponibili       : %d Philia\n", ledger-reserved)
			fmt.Println("------------------")

		case "send":
			if currentUserIdentity == "" {
				fmt.Println("❌ Identità non caricata.")
				continue
			}
			if len(parts) != 4 {
				fmt.Println("Uso: send <ID_destinatario> <importo> <memo>")
				continue
			}
			destID := parts[1]
			amount := parseInt(parts[2])
			memo := parts[3]

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			
			ev := NewEvent(PAYMENT, parent, myID, destID, amount, time.Now().UnixNano(), memo)
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("❌ Errore invio: %v\n", err)
			} else {
				fmt.Printf("✅ Pagamento PRENOTATO: %d Philia inviati a %s...\n", amount, destID[:8])
				fmt.Println("   (Usa 'settle' per confermare il trasferimento definitivo)")
			}

		case "settle":
			if currentUserIdentity == "" {
				fmt.Println("❌ Identità non caricata.")
				continue
			}
			if len(parts) != 3 {
				fmt.Println("Uso: settle <ID_destinatario> <importo>")
				continue
			}
			destID := parts[1]
			amount := parseInt(parts[2])

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			
			// Nel wallet generico, il reference può essere l'ID del destinatario o un ID transazione
			ev := NewEvent(SETTLE, parent, myID, destID, amount, time.Now().UnixNano(), "Settlement for "+destID[:8])
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("❌ Errore regolamento: %v\n", err)
			} else {
				fmt.Printf("✅ Pagamento REGOLATO: %d Philia trasferiti definitivamente a %s...\n", amount, destID[:8])
			}

		case "anchor":
			logPath := filepath.Join("data", "events.log")
			anchorPath := filepath.Join("data", "anchors.json")
			today := time.Now().Format("2006-01-02")
			if err := ExecuteDailyAnchor(logPath, anchorPath, today); err != nil {
				fmt.Printf("❌ Ancoraggio fallito: %v\n", err)
			}

		case "verify":
			anchorPath := filepath.Join("data", "anchors.json")
			logPath := filepath.Join("data", "events.log")
			
			data, err := os.ReadFile(anchorPath)
			if err != nil {
				fmt.Println("❌ Nessun file di ancoraggio trovato.")
				continue
			}
			
			var records []AnchorRecord
			if err := json.Unmarshal(data, &records); err != nil || len(records) == 0 {
				fmt.Println("❌ Nessun record valido.")
				continue
			}
			
			lastRecord := records[len(records)-1]
			currentData, _ := os.ReadFile(logPath)
			currentHashBytes := sha256.Sum256(currentData)
			currentHash := hex.EncodeToString(currentHashBytes[:])
			
			fmt.Println("🛡️ Verifica integrità...")
			if currentHash == lastRecord.LogHash {
				fmt.Println("✅ INTEGRITÀ CONFERMATA: Registro immutato.")
			} else {
				fmt.Println("🚨 ALLARME: Hash non corrispondenti!")
			}

		case "exit":
			fmt.Println("💾 Salvataggio in corso...")
			engine.Close()
			fmt.Println("👋 Uscita sicura. La tua chiave privata è stata eliminata dalla memoria.")
			return

		default:
			fmt.Println("❌ Comando non riconosciuto.")
		}
	}
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}