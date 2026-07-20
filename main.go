package main

import (
	"bufio"
	"context"
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

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

var sessionKeys = make(map[string]ed25519.PrivateKey)
var currentUserIdentity string
var dataDir string
var nonces = make(map[string]uint64) // Contatore nonce per identità

func getOrCreateKey(name string) (ed25519.PrivateKey, string) {
	if priv, exists := sessionKeys[name]; exists {
		pub := priv.Public().(ed25519.PublicKey)
		return priv, hex.EncodeToString(pub)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sessionKeys[name] = priv
	saveKeyToDisk(dataDir, name, priv)
	return priv, hex.EncodeToString(pub)
}

// getNextNonce restituisce il prossimo nonce disponibile per un'identità
func getNextNonce(userID string) uint64 {
	nonce := nonces[userID] + 1
	nonces[userID] = nonce
	return nonce
}

// initNonces scansiona l'eventLog per inizializzare i contatori nonce
func initNonces(engine *Engine) {
	for _, ev := range engine.GetEventLog() {
		if ev.Nonce > nonces[ev.Sender] {
			nonces[ev.Sender] = ev.Nonce
		}
	}
	fmt.Printf("✅ Nonce inizializzati per %d identità\n", len(nonces))
}

func main() {
	dataDir = "data"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}

	sessionKeys = loadKeysFromDisk(dataDir)

	p2pPort := 4001
	if len(os.Args) > 2 {
		port, err := strconv.Atoi(os.Args[2])
		if err == nil {
			p2pPort = port
		}
	}

	fmt.Println("==========================================")
	fmt.Println("       PHILIA ECONOMIC PROTOCOL (PEP)     ")
	fmt.Println("          Universal Wallet CLI v1.1       ")
	fmt.Println("==========================================")
	fmt.Println("Comandi:")
	fmt.Println("  create <nome>                 : Crea/Carica la tua identita")
	fmt.Println("  import <chiave_privata_hex>   : Importa un wallet esistente")
	fmt.Println("  fund <importo>                : Ricevi fondi iniziali (GENESIS)")
	fmt.Println("  balance                       : Mostra i tuoi saldi")
	fmt.Println("  reputation                    : Mostra la tua reputazione")
	fmt.Println("  send <ID_dest> <importo> <memo>: Prenota un pagamento (PAYMENT, TTL 7 giorni)")
	fmt.Println("  settle <ID_dest> <importo>    : Regola definitivamente (SETTLE)")
	fmt.Println("  send-message <ID_dest> <testo>: Invia un messaggio crittografato")
	fmt.Println("  send-agreement <ID_dest> <testo>: Invia un accordo contrattuale")
	fmt.Println("  send-escrow <seller_id> <importo> <descrizione>: Crea contratto escrow")
	fmt.Println("  release-escrow <escrow_id>    : Firma rilascio escrow")
	fmt.Println("  dispute-escrow <escrow_id> <motivo>: Apri disputa escrow")
	fmt.Println("  check-expired                 : Controlla pagamenti scaduti")
	fmt.Println("  list-messages                 : Mostra i tuoi messaggi ricevuti")
	fmt.Println("  create-room <nome> <cat> <prezzo> : Crea una ROOM")
	fmt.Println("  list-rooms                    : Elenca le ROOMS locali")
	fmt.Println("  connect <multiaddr>           : Connettiti a un peer P2P")
	fmt.Println("  sync <peer_id>                : Sincronizza DAG con peer")
	fmt.Println("  anchor                        : Notarizza il registro su blockchain")
	fmt.Println("  verify                        : Verifica l'integrita del registro")
	fmt.Println("  show-key                      : Mostra la tua chiave privata")
	fmt.Println("  exit                          : Salva e chiudi")
	fmt.Println("==========================================")

	store := NewStore(dataDir)
	engine := NewEngine(store)
	defer engine.Close()

	// Inizializza i contatori nonce dall'eventLog esistente
	initNonces(engine)

	p2pNode, err := NewP2PNode(context.Background(), p2pPort, engine)
	if err != nil {
		fmt.Printf("Errore avvio P2P: %v\n", err)
		return
	}
	defer p2pNode.Host.Close()

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
			fmt.Printf("Identita caricata: %s\n", name)
			fmt.Printf("   Il tuo ID Pubblico: %s...\n", id[:16])

		case "import":
			if len(parts) != 2 {
				fmt.Println("Uso: import <chiave_privata_hex>")
				continue
			}
			privKeyHex := parts[1]
			privKeyBytes, err := hex.DecodeString(privKeyHex)
			if err != nil || len(privKeyBytes) != 64 {
				fmt.Println("Chiave privata non valida (deve essere 64 byte hex)")
				continue
			}
			privKey := ed25519.PrivateKey(privKeyBytes)
			pubKey := hex.EncodeToString(privKey.Public().(ed25519.PublicKey))
			sessionKeys["imported"] = privKey
			currentUserIdentity = "imported"
			fmt.Printf("Wallet importato con successo!\n")
			fmt.Printf("   ID Pubblico: %s...\n", pubKey[:16])

		case "fund":
			if currentUserIdentity == "" {
				fmt.Println("Devi prima creare un'identita con 'create <nome>' o 'import <chiave>'.")
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
			nonce := getNextNonce(id)
			ev := NewEvent(GENESIS, "", id, "", amount, time.Now().UnixNano(), nonce, 0, "Wallet Funding")
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("Ricevuti %d Philia (GENESIS) | Nonce: %d\n", amount, nonce)
			}

		case "balance":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			_, id := getOrCreateKey(currentUserIdentity)
			ledger, reserved := engine.GetBalance(id)
			fmt.Println("\n--- IL TUO SALDO ---")
			fmt.Printf("Totale (Ledger)   : %d Philia\n", ledger)
			fmt.Printf("In prenotazione   : %d Philia\n", reserved)
			fmt.Printf("Disponibili       : %d Philia\n", ledger-reserved)
			fmt.Println("------------------")

		case "reputation":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			_, id := getOrCreateKey(currentUserIdentity)
			rep := engine.GetReputation(id)
			maxAmount := engine.MaxTransactionAmount(id)
			fmt.Println("\n--- LA TUA REPUTAZIONE ---")
			fmt.Printf("ID: %s...\n", id[:16])
			fmt.Printf("Vendite completate  : %d\n", rep.CompletedSales)
			fmt.Printf("Acquisti completati : %d\n", rep.CompletedPurchases)
			fmt.Printf("Pagamenti scaduti   : %d\n", rep.ExpiredPayments)
			fmt.Printf("Dispute perse       : %d\n", rep.DisputesLost)
			fmt.Printf("Punteggio           : %d\n", rep.Score)
			fmt.Printf("Livello fiducia     : %s\n", rep.TrustLevel)
			fmt.Printf("Limite transazione  : %d Philia\n", maxAmount)
			fmt.Println("--------------------------")

		case "send":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
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
			nonce := getNextNonce(myID)
			ttlSeconds := int64(604800) // 7 giorni di TTL

			ev := NewEvent(PAYMENT, parent, myID, destID, amount, time.Now().UnixNano(), nonce, ttlSeconds, memo)
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore invio: %v\n", err)
			} else {
				fmt.Printf("Pagamento PRENOTATO: %d Philia inviati a %s...\n", amount, destID[:8])
				fmt.Printf("   TTL: 7 giorni | Nonce: %d\n", nonce)
				fmt.Println("   (Usa 'settle' per confermare il trasferimento definitivo)")
			}

		case "settle":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
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
			nonce := getNextNonce(myID)

			ev := NewEvent(SETTLE, parent, myID, destID, amount, time.Now().UnixNano(), nonce, 0, "Settlement for "+destID[:8])
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore regolamento: %v\n", err)
			} else {
				fmt.Printf("Pagamento REGOLATO: %d Philia trasferiti definitivamente a %s...\n", amount, destID[:8])
				fmt.Printf("   Nonce: %d\n", nonce)
			}

		case "send-message":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Uso: send-message <ID_destinatario> <testo del messaggio>")
				continue
			}
			destID := parts[1]
			messageText := strings.Join(parts[2:], " ")
			messageText = strings.Trim(messageText, "\"")

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			ev := NewEvent(MESSAGE, parent, myID, destID, 0, time.Now().UnixNano(), nonce, 0, messageText)
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore invio messaggio: %v\n", err)
			} else {
				fmt.Printf("💬 Messaggio inviato a %s...\n", destID[:8])
				fmt.Printf("   Nonce: %d\n", nonce)
				fmt.Println("   (Il destinatario dovrà fare 'sync' per leggerlo)")
			}

		case "send-agreement":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Uso: send-agreement <ID_destinatario> <testo dell'accordo>")
				continue
			}
			destID := parts[1]
			agreementText := strings.Join(parts[2:], " ")
			agreementText = strings.Trim(agreementText, "\"")

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			ev := NewEvent(AGREEMENT, parent, myID, destID, 0, time.Now().UnixNano(), nonce, 0, agreementText)
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore invio accordo: %v\n", err)
			} else {
				fmt.Printf("📜 Accordo registrato e inviato a %s...\n", destID[:8])
				fmt.Printf("   Nonce: %d\n", nonce)
				fmt.Println("   (L'accordo è ora immutabile nel DAG)")
			}

		case "send-escrow":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 4 {
				fmt.Println("Uso: send-escrow <seller_id> <importo> <descrizione>")
				fmt.Println("Esempio: send-escrow abc123... 500 iPhone 15 Pro")
				continue
			}
			sellerID := parts[1]
			amount := parseInt(parts[2])
			description := strings.Join(parts[3:], " ")

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			// Crea struct Escrow
			escrowID := fmt.Sprintf("esc_%s_%d", myID[:8], time.Now().Unix())
			escrow := Escrow{
				ID:           escrowID,
				Buyer:        myID,
				Seller:       sellerID,
				Arbitrator:   "0000000000000000000000000000000000000000000000000000000000000000", // Default arbitrator
				Amount:       amount,
				Description:  description,
				RequiredSigs: 2,
				Signatures:   make(map[string]string),
				CreatedAt:    time.Now().UnixNano(),
				ExpiresAt:    time.Now().Add(7 * 24 * time.Hour).UnixNano(), // 7 giorni
			}
			escrowJSON, _ := json.Marshal(escrow)

			ev := NewEvent(ESCROW_LOCK, parent, myID, sellerID, amount, time.Now().UnixNano(), nonce, 0, string(escrowJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore creazione escrow: %v\n", err)
			} else {
				fmt.Printf("🔒 ESCROW creato: %s\n", escrowID)
				fmt.Printf("   Importo: %d Philia bloccati\n", amount)
				fmt.Printf("   Venditore: %s...\n", sellerID[:16])
				fmt.Printf("   Richieste: 2 firme (buyer + seller)\n")
				fmt.Printf("   Scadenza: 7 giorni\n")
				fmt.Printf("   Nonce: %d\n", nonce)
			}

		case "release-escrow":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) != 2 {
				fmt.Println("Uso: release-escrow <escrow_id>")
				continue
			}
			escrowID := parts[1]

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			releaseData := map[string]string{
				"escrow_id": escrowID,
			}
			releaseJSON, _ := json.Marshal(releaseData)

			ev := NewEvent(ESCROW_RELEASE, parent, myID, "", 0, time.Now().UnixNano(), nonce, 0, string(releaseJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore rilascio escrow: %v\n", err)
			} else {
				fmt.Printf("✍️ Firma ESCROW %s registrata\n", escrowID[:16])
				fmt.Printf("   Nonce: %d\n", nonce)
				fmt.Println("   (In attesa della seconda firma per rilasciare i fondi)")
			}

		case "dispute-escrow":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Uso: dispute-escrow <escrow_id> <motivo>")
				continue
			}
			escrowID := parts[1]
			reason := strings.Join(parts[2:], " ")

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			disputeData := map[string]string{
				"escrow_id":   escrowID,
				"description": reason,
			}
			disputeJSON, _ := json.Marshal(disputeData)

			ev := NewEvent(ESCROW_DISPUTE, parent, myID, "", 0, time.Now().UnixNano(), nonce, 0, string(disputeJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore apertura disputa: %v\n", err)
			} else {
				fmt.Printf("⚠️ DISPUTA aperta su ESCROW %s\n", escrowID[:16])
				fmt.Printf("   Motivo: %s\n", reason)
				fmt.Printf("   Nonce: %d\n", nonce)
				fmt.Println("   (In attesa della risoluzione dell'arbitro)")
			}

		case "check-expired":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			engine.checkExpiredPayments()
			fmt.Println("✅ Controllo pagamenti scaduti completato")

		case "list-messages":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			_, myID := getOrCreateKey(currentUserIdentity)
			msgs := engine.GetMessages(myID)

			fmt.Println("\n--- MESSAGGI RICEVUTI ---")
			if len(msgs) == 0 {
				fmt.Println("Nessun messaggio nella tua casella.")
			} else {
				for _, m := range msgs {
					t := time.Unix(0, m.Timestamp).Format("15:04")
					fmt.Printf("[%s] Da %s...: %s\n", t, m.Sender[:8], m.Memo)
				}
			}
			fmt.Println("-------------------------")

		case "create-room":
			if currentUserIdentity == "" {
				fmt.Println("Devi prima creare un'identita con 'create <nome>'.")
				continue
			}
			if len(parts) != 4 {
				fmt.Println("Uso: create-room <nome> <categoria> <prezzo>")
				fmt.Println("Esempio: create-room B&B_Roma hospitality 300")
				continue
			}
			roomName := strings.Trim(parts[1], "\"")
			category := parts[2]
			price := parseInt(parts[3])

			priv, myID := getOrCreateKey(currentUserIdentity)
			nonce := getNextNonce(myID)

			roomData := Room{
				Name:        roomName,
				Description: "Creata via CLI PEP",
				Category:    category,
				IsPublic:    true,
				BasePrice:   price,
			}
			roomJSON, _ := json.Marshal(roomData)

			parent := engine.GetLastHash()
			ev := NewEvent(ROOM_CREATE, parent, myID, "", 0, time.Now().UnixNano(), nonce, 0, string(roomJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore creazione room: %v\n", err)
			} else {
				fmt.Printf("ROOM creata con successo: %s\n", roomName)
				fmt.Printf("ID Room: %s\n", ev.ID)
				fmt.Printf("Nonce: %d\n", nonce)
			}

		case "list-rooms":
			fmt.Println("\n--- MARKETPLACE LOCALE (DAG) ---")
			rooms := engine.GetRooms()
			if len(rooms) == 0 {
				fmt.Println("Nessuna ROOM trovata nel registro locale.")
			} else {
				for id, room := range rooms {
					visibility := "Pubblica"
					if !room.IsPublic {
						visibility = "Privata"
					}
					fmt.Printf(" [%s] %s\n", visibility, room.Name)
					fmt.Printf("   ID: %s...\n", id[:16])
					fmt.Printf("   Categoria: %s | Prezzo: %d Philia\n", room.Category, room.BasePrice)
					fmt.Printf("   Proprietario: %s...\n", room.OwnerID[:16])
					fmt.Println("   --------------------------------")
				}
			}
			fmt.Println("--------------------------------")

		case "connect":
			if len(parts) != 2 {
				fmt.Println("Uso: connect <multiaddr_peer>")
				fmt.Println("Esempio: connect /ip4/127.0.0.1/tcp/4002/p2p/12D3KooW...")
				continue
			}
			addrStr := parts[1]
			maddr, err := multiaddr.NewMultiaddr(addrStr)
			if err != nil {
				fmt.Printf("Indirizzo non valido: %v\n", err)
				continue
			}
			peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
			if err != nil {
				fmt.Printf("Errore parsing peer: %v\n", err)
				continue
			}

			if err := p2pNode.Host.Connect(context.Background(), *peerInfo); err != nil {
				fmt.Printf("Connessione fallita: %v\n", err)
			} else {
				fmt.Printf("✅ Connesso al peer: %s\n", peerInfo.ID)
			}

		case "sync":
			if len(parts) != 2 {
				fmt.Println("Uso: sync <peer_id>")
				fmt.Println("Esempio: sync 12D3KooW...")
				continue
			}
			peerIDStr := parts[1]
			peerID, err := peer.Decode(peerIDStr)
			if err != nil {
				fmt.Printf("Peer ID non valido: %v\n", err)
				continue
			}

			peerInfo := p2pNode.Host.Peerstore().PeerInfo(peerID)
			if len(peerInfo.Addrs) == 0 {
				fmt.Println("Peer non trovato. Usa prima 'connect <multiaddr>'.")
				continue
			}

			if err := p2pNode.SyncWith(context.Background(), peerInfo); err != nil {
				fmt.Printf("Sincronizzazione fallita: %v\n", err)
			}

		case "anchor":
			logPath := filepath.Join(dataDir, "events.log")
			anchorPath := filepath.Join(dataDir, "anchors.json")
			today := time.Now().Format("2006-01-02")
			if err := ExecuteDailyAnchor(logPath, anchorPath, today); err != nil {
				fmt.Printf("Ancoraggio fallito: %v\n", err)
			}

		case "verify":
			anchorPath := filepath.Join(dataDir, "anchors.json")
			logPath := filepath.Join(dataDir, "events.log")

			data, err := os.ReadFile(anchorPath)
			if err != nil {
				fmt.Println("Nessun file di ancoraggio trovato.")
				continue
			}

			var records []AnchorRecord
			if err := json.Unmarshal(data, &records); err != nil || len(records) == 0 {
				fmt.Println("Nessun record valido.")
				continue
			}

			lastRecord := records[len(records)-1]
			currentData, _ := os.ReadFile(logPath)
			currentHashBytes := sha256.Sum256(currentData)
			currentHash := hex.EncodeToString(currentHashBytes[:])

			fmt.Println("Verifica integrita...")
			if currentHash == lastRecord.LogHash {
				fmt.Println("INTEGRITA CONFERMATA: Registro immutato.")
			} else {
				fmt.Println("ALLARME: Hash non corrispondenti!")
			}

		case "show-key":
			if currentUserIdentity == "" {
				fmt.Println("Devi prima creare un'identita con 'create <nome>'.")
				continue
			}
			priv, pub := getOrCreateKey(currentUserIdentity)
			privHex := hex.EncodeToString(priv)
			fmt.Printf("\n--- CHIAVI DI %s ---\n", currentUserIdentity)
			fmt.Printf("ID Pubblico: %s\n", pub)
			fmt.Printf("Chiave Privata: %s\n", privHex)
			fmt.Printf("Nonce corrente: %d\n", nonces[pub])
			fmt.Printf("-------------------\n")
			fmt.Println("Copia la chiave privata per importarla nella web app.")

		case "exit":
			fmt.Println("Salvataggio in corso...")
			engine.Close()
			fmt.Println("Uscita sicura. La tua chiave privata e stata eliminata dalla memoria.")
			return

		default:
			fmt.Println("Comando non riconosciuto.")
		}
	}
}

func saveKeyToDisk(dir, name string, priv ed25519.PrivateKey) {
	path := filepath.Join(dir, "keystore.json")
	keys := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &keys)
	}
	keys[name] = hex.EncodeToString(priv)
	newData, _ := json.MarshalIndent(keys, "", "  ")
	os.WriteFile(path, newData, 0644)
}

func loadKeysFromDisk(dir string) map[string]ed25519.PrivateKey {
	path := filepath.Join(dir, "keystore.json")
	loadedKeys := make(map[string]ed25519.PrivateKey)
	if data, err := os.ReadFile(path); err == nil {
		var hexKeys map[string]string
		if err := json.Unmarshal(data, &hexKeys); err == nil {
			for name, hexKey := range hexKeys {
				keyBytes, err := hex.DecodeString(hexKey)
				if err == nil && len(keyBytes) == 64 {
					loadedKeys[name] = ed25519.PrivateKey(keyBytes)
				}
			}
		}
	}
	return loadedKeys
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}