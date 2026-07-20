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
	fmt.Println("       PEP HOTEL POS - INTERFACCIA CLI    ")
	fmt.Println("==========================================")
	fmt.Println("Comandi disponibili:")
	fmt.Println("  fund <nome> <importo>             : Crea fondi iniziali (GENESIS)")
	fmt.Println("  check-in <ospite> <stanza> <imp>  : Prenota stanza (PAYMENT)")
	fmt.Println("  check-out <ospite> <stanza> <imp> : Regola il pagamento (SETTLE)")
	fmt.Println("  status                            : Mostra i saldi attuali")
	fmt.Println("  anchor                            : Ancora il registro di oggi")
	fmt.Println("  verify                            : Verifica l'integrita del registro")
	fmt.Println("  exit                              : Salva e chiudi il sistema")
	fmt.Println("==========================================")

	store := NewStore("data")
	engine := NewEngine(store)
	defer engine.Close()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("\npep> ")
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
		case "fund":
			if len(parts) != 3 {
				fmt.Println("Uso: fund <nome> <importo>")
				continue
			}
			name := parts[1]
			amount := parseInt(parts[2])
			if amount <= 0 {
				fmt.Println("L'importo deve essere positivo.")
				continue
			}
			priv, id := getOrCreateKey(name)
			ev := NewEvent(GENESIS, "", id, "", amount, time.Now().UnixNano(), "CLI Funding")
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("Fondi creati per %s (ID: %s...)\n", name, id[:8])
			}

		case "check-in":
			if len(parts) != 4 {
				fmt.Println("Uso: check-in <nome_ospite> <stanza> <importo>")
				continue
			}
			guestName := parts[1]
			room := parts[2]
			amount := parseInt(parts[3])
			
			priv, guestID := getOrCreateKey(guestName)
			_, hotelID := getOrCreateKey("hotel")
			
			parent := engine.GetLastHash()
			ev := NewEvent(PAYMENT, parent, guestID, hotelID, amount, time.Now().UnixNano(), room)
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore check-in: %v\n", err)
			} else {
				fmt.Printf("Check-in riuscito: %s ha prenotato %s per %d Philia.\n", guestName, room, amount)
			}

		case "check-out":
			if len(parts) != 4 {
				fmt.Println("Uso: check-out <nome_ospite> <stanza> <importo>")
				continue
			}
			guestName := parts[1]
			room := parts[2]
			amount := parseInt(parts[3])
			
			priv, guestID := getOrCreateKey(guestName)
			_, hotelID := getOrCreateKey("hotel")
			
			parent := engine.GetLastHash()
			ev := NewEvent(SETTLE, parent, guestID, hotelID, amount, time.Now().UnixNano(), room)
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore check-out: %v\n", err)
			} else {
				fmt.Printf("Check-out riuscito: %d Philia trasferiti per la stanza %s.\n", amount, room)
			}

		case "status":
			fmt.Println("\n--- STATO ATTUALE DEL LEDGER ---")
			for name, priv := range sessionKeys {
				id := hex.EncodeToString(priv.Public().(ed25519.PublicKey))
				ledger, reserved := engine.GetBalance(id)
				fmt.Printf("%-10s (ID: %s...): Ledger=%4d, Reserved=%4d, Disponibili=%4d\n", 
					name, id[:8], ledger, reserved, ledger-reserved)
			}
			fmt.Println("--------------------------------")

		case "anchor":
			logPath := filepath.Join("data", "events.log")
			anchorPath := filepath.Join("data", "anchors.json")
			today := time.Now().Format("2006-01-02")
			if err := ExecuteDailyAnchor(logPath, anchorPath, today); err != nil {
				fmt.Printf("Ancoraggio fallito: %v\n", err)
			}

		case "verify":
			anchorPath := filepath.Join("data", "anchors.json")
			logPath := filepath.Join("data", "events.log")
			
			data, err := os.ReadFile(anchorPath)
			if err != nil {
				fmt.Println("Nessun file di ancoraggio trovato. Esegui prima 'anchor'.")
				continue
			}
			
			var records []AnchorRecord
			if err := json.Unmarshal(data, &records); err != nil || len(records) == 0 {
				fmt.Println("Nessun record di ancoraggio valido trovato.")
				continue
			}
			
			lastRecord := records[len(records)-1]
			
			currentData, err := os.ReadFile(logPath)
			if err != nil {
				fmt.Println("Impossibile leggere events.log")
				continue
			}
			
			currentHashBytes := sha256.Sum256(currentData)
			currentHash := hex.EncodeToString(currentHashBytes[:])
			
			fmt.Println("Verifica integrita in corso...")
			fmt.Printf("Hash nel sigillo: %s\n", lastRecord.LogHash[:16])
			fmt.Printf("Hash attuale:     %s\n", currentHash[:16])
			
			if currentHash == lastRecord.LogHash {
				fmt.Println("INTEGRITA CONFERMATA: Il file non e stato alterato.")
			} else {
				fmt.Println("ALLARME MANOMISSIONE: Gli hash non corrispondono!")
			}

		case "exit":
			fmt.Println("Salvataggio stato finale in corso...")
			engine.Close()
			fmt.Println("Arrivederci. Il sistema PEP e stato spento in modo sicuro.")
			return

		default:
			fmt.Println("Comando non riconosciuto. Digita i comandi elencati sopra.")
		}
	}
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}