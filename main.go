package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
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
var nonces = make(map[string]uint64)
var x25519Keys = make(map[string]struct{ priv, pub []byte })

func getOrCreateX25519Key(name string) ([]byte, []byte) {
	if k, exists := x25519Keys[name]; exists {
		return k.priv, k.pub
	}
	
	path := filepath.Join(dataDir, "keystore.json")
	keys := make(map[string]string)
	
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &keys)
	}
	
	if privHex, ok := keys[name+"_x25519_priv"]; ok {
		if pubHex, ok2 := keys[name+"_x25519_pub"]; ok2 {
			priv, _ := hex.DecodeString(privHex)
			pub, _ := hex.DecodeString(pubHex)
			x25519Keys[name] = struct{ priv, pub []byte }{priv, pub}
			return priv, pub
		}
	}
	
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		fmt.Printf("Errore generazione chiave X25519: %v\n", err)
		return nil, nil
	}
	x25519Keys[name] = struct{ priv, pub []byte }{priv, pub}
	
	keys[name+"_x25519_priv"] = hex.EncodeToString(priv)
	keys[name+"_x25519_pub"] = hex.EncodeToString(pub)
	newData, _ := json.MarshalIndent(keys, "", "  ")
	os.WriteFile(path, newData, 0644)
	
	return priv, pub
}

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

func getNextNonce(userID string) uint64 {
	nonce := nonces[userID] + 1
	nonces[userID] = nonce
	return nonce
}

func initNonces(engine *Engine) {
	for _, ev := range engine.GetEventLog() {
		if ev.Nonce > nonces[ev.Sender] {
			nonces[ev.Sender] = ev.Nonce
		}
	}
	fmt.Printf("✅ Nonce inizializzati per %d identità\n", len(nonces))
}

func main() {
	flag.StringVar(&dataDir, "data-dir", "data", "Directory per i dati del nodo")
	p2pPort := flag.Int("port", 4001, "Porta per la connessione P2P")
	flag.Parse()

	fmt.Printf("📂 Directory dati: %s | Porta P2P: %d\n", dataDir, *p2pPort)

	sessionKeys = loadKeysFromDisk(dataDir)

	fmt.Println("==========================================")
	fmt.Println("       PHILIA ECONOMIC PROTOCOL (PEP)     ")
	fmt.Println("          Universal Wallet CLI v1.2 (E2E) ")
	fmt.Println("==========================================")
	fmt.Println("Comandi: create, import, fund, balance, reputation, send, settle,")
	fmt.Println("         send-message, send-agreement, send-escrow, release-escrow,")
	fmt.Println("         dispute-escrow, check-expired, list-messages, chat,")
	fmt.Println("         create-room, list-rooms, connect, sync, anchor, verify, show-key, exit")
	fmt.Println("==========================================")

	store := NewStore(dataDir)
	engine := NewEngine(store)
	defer engine.Close()

	initNonces(engine)

	p2pNode, err := NewP2PNode(context.Background(), *p2pPort, engine)
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
			priv, id := getOrCreateKey(name)
			_, xPub := getOrCreateX25519Key(name)
			currentUserIdentity = name

			parent := engine.GetLastHash()
			nonce := getNextNonce(id)
			announceEv := NewEvent("KEY_ANNOUNCE", parent, id, "", 0, time.Now().UnixNano(), nonce, 0, hex.EncodeToString(xPub))
			announceEv.Sign(priv)
			engine.ProcessEvent(announceEv)

			fmt.Printf("Identita caricata: %s\n", name)
			fmt.Printf("   Il tuo ID Pubblico: %s...\n", id[:16])
			fmt.Println("   ✅ Chiave di crittografia E2E annunciata nel DAG")

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
			fmt.Printf("Wallet importato con successo!\n   ID Pubblico: %s...\n", pubKey[:16])

		case "fund":
			if currentUserIdentity == "" {
				fmt.Println("Devi prima creare un'identita.")
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
			ttlSeconds := int64(604800)

			ev := NewEvent(PAYMENT, parent, myID, destID, amount, time.Now().UnixNano(), nonce, ttlSeconds, memo)
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore invio: %v\n", err)
			} else {
				fmt.Printf("Pagamento PRENOTATO: %d Philia inviati a %s...\n", amount, destID[:8])
				fmt.Printf("   TTL: 7 giorni | Nonce: %d\n", nonce)
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

			ev := NewEvent(SETTLE, parent, myID, destID, amount, time.Now().UnixNano(), nonce, 0, "Settlement")
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore regolamento: %v\n", err)
			} else {
				fmt.Printf("Pagamento REGOLATO: %d Philia trasferiti a %s...\n", amount, destID[:8])
			}

		case "send-message":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Uso: send-message <ID_destinatario> <testo>")
				continue
			}
			destID := parts[1]
			messageText := strings.Join(parts[2:], " ")
			messageText = strings.Trim(messageText, "\"")

			priv, myID := getOrCreateKey(currentUserIdentity)
			myXPriv, _ := getOrCreateX25519Key(currentUserIdentity)
			
			destXPub := engine.GetX25519PubKey(destID)
			if len(destXPub) == 0 {
				fmt.Println("⚠️ Chiave X25519 del destinatario non trovata.")
				fmt.Println("   Assicurati che il destinatario abbia eseguito 'create' e che tu abbia fatto 'sync'.")
				continue
			}

			sharedSecret, err := ComputeSharedSecret(myXPriv, destXPub)
			if err != nil {
				fmt.Printf("Errore calcolo segreto: %v\n", err)
				continue
			}

			ciphertext, err := EncryptMessage(sharedSecret, []byte(messageText))
			if err != nil {
				fmt.Printf("Errore crittografia: %v\n", err)
				continue
			}

			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			ev := NewEvent(MESSAGE, parent, myID, destID, 0, time.Now().UnixNano(), nonce, 0, hex.EncodeToString(ciphertext))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore invio messaggio: %v\n", err)
			} else {
				fmt.Printf("🔒 Messaggio E2E inviato a %s...\n", destID[:8])
			}

		case "send-agreement":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Uso: send-agreement <ID_destinatario> <testo>")
				continue
			}
			destID := parts[1]
			agreementText := strings.Join(parts[2:], " ")
			
			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			ev := NewEvent(AGREEMENT, parent, myID, destID, 0, time.Now().UnixNano(), nonce, 0, agreementText)
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("📜 Accordo registrato e inviato a %s...\n", destID[:8])
			}

		case "send-escrow":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 4 {
				fmt.Println("Uso: send-escrow <seller_id> <importo> <descrizione>")
				continue
			}
			sellerID := parts[1]
			amount := parseInt(parts[2])
			description := strings.Join(parts[3:], " ")

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			escrowID := fmt.Sprintf("esc_%s_%d", myID[:8], time.Now().Unix())
			escrow := Escrow{
				ID: escrowID, Buyer: myID, Seller: sellerID,
				Arbitrator: "0000000000000000000000000000000000000000000000000000000000000000",
				Amount: amount, Description: description, RequiredSigs: 2,
				Signatures: make(map[string]string),
				CreatedAt: time.Now().UnixNano(),
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour).UnixNano(),
			}
			escrowJSON, _ := json.Marshal(escrow)

			ev := NewEvent(ESCROW_LOCK, parent, myID, sellerID, amount, time.Now().UnixNano(), nonce, 0, string(escrowJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore creazione escrow: %v\n", err)
			} else {
				fmt.Printf("🔒 ESCROW creato: %s\n", escrowID)
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

			releaseData := map[string]string{"escrow_id": escrowID}
			releaseJSON, _ := json.Marshal(releaseData)

			ev := NewEvent(ESCROW_RELEASE, parent, myID, "", 0, time.Now().UnixNano(), nonce, 0, string(releaseJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("✍️ Firma ESCROW %s registrata\n", escrowID[:16])
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

			disputeData := map[string]string{"escrow_id": escrowID, "description": reason}
			disputeJSON, _ := json.Marshal(disputeData)

			ev := NewEvent(ESCROW_DISPUTE, parent, myID, "", 0, time.Now().UnixNano(), nonce, 0, string(disputeJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("⚠️ DISPUTA aperta su ESCROW %s\n", escrowID[:16])
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
				fmt.Println("Nessun messaggio.")
			} else {
				for _, m := range msgs {
					t := time.Unix(0, m.Timestamp).Format("15:04")
					fmt.Printf("[%s] Da %s...: %s\n", t, m.Sender[:8], m.Memo)
				}
			}
			fmt.Println("-------------------------")

		case "chat":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) < 2 {
				fmt.Println("Uso: chat <ID_contatto>")
				continue
			}
			contactID := parts[1]
			_, myID := getOrCreateKey(currentUserIdentity)
			myXPriv, _ := getOrCreateX25519Key(currentUserIdentity)
			
			chatEvents := engine.SyncRecentEvents(myID, contactID, 50)
			
			fmt.Printf("\n💬 Chat con %s... (ultimi %d messaggi)\n", contactID[:16], len(chatEvents))
			fmt.Println("─────────────────────────────────────")
			
			if len(chatEvents) == 0 {
				fmt.Println("Nessun messaggio in questa conversazione.")
			} else {
				for _, msg := range chatEvents {
					t := time.Unix(0, msg.Timestamp).Format("15:04")
					direction := "→"
					if msg.Sender == myID {
						direction = "←"
					}
					
					senderName := msg.Sender[:8]
					if msg.Sender == myID {
						senderName = "TU"
					}
					
					displayText := msg.Memo
					ciphertext, err := hex.DecodeString(msg.Memo)
					if err == nil && len(ciphertext) > 0 {
						otherXPub := engine.GetX25519PubKey(contactID)
						if len(otherXPub) > 0 {
							sharedSecret, err := ComputeSharedSecret(myXPriv, otherXPub)
							if err == nil {
								plaintext, err := DecryptMessage(sharedSecret, ciphertext)
								if err == nil {
									displayText = string(plaintext)
								} else {
									displayText = "[Errore decriptazione]"
								}
							}
						} else {
							displayText = "[Chiave interlocutore mancante]"
						}
					}
					
					fmt.Printf("[%s] %s %-8s: %s\n", t, direction, senderName, displayText)
				}
			}
			fmt.Println("─────────────────────────────────────")

		case "create-room":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			if len(parts) != 4 {
				fmt.Println("Uso: create-room <nome> <categoria> <prezzo>")
				continue
			}
			roomName := strings.Trim(parts[1], "\"")
			category := parts[2]
			price := parseInt(parts[3])

			priv, myID := getOrCreateKey(currentUserIdentity)
			nonce := getNextNonce(myID)

			roomData := Room{Name: roomName, Description: "Creata via CLI PEP", Category: category, IsPublic: true, BasePrice: price}
			roomJSON, _ := json.Marshal(roomData)

			parent := engine.GetLastHash()
			ev := NewEvent(ROOM_CREATE, parent, myID, "", 0, time.Now().UnixNano(), nonce, 0, string(roomJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("ROOM creata: %s\n", roomName)
			}

		case "list-rooms":
			fmt.Println("\n--- MARKETPLACE LOCALE (DAG) ---")
			rooms := engine.GetRooms()
			if len(rooms) == 0 {
				fmt.Println("Nessuna ROOM trovata.")
			} else {
				for id, room := range rooms {
					visibility := "Pubblica"
					if !room.IsPublic { visibility = "Privata" }
					fmt.Printf(" [%s] %s (ID: %s...)\n", visibility, room.Name, id[:16])
					fmt.Printf("   Categoria: %s | Prezzo: %d Philia\n", room.Category, room.BasePrice)
					fmt.Println("   --------------------------------")
				}
			}

		case "connect":
			if len(parts) != 2 {
				fmt.Println("Uso: connect <multiaddr_peer>")
				continue
			}
			maddr, err := multiaddr.NewMultiaddr(parts[1])
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
				continue
			}
			peerID, err := peer.Decode(parts[1])
			if err != nil {
				fmt.Printf("Peer ID non valido: %v\n", err)
				continue
			}

			peerInfo := p2pNode.Host.Peerstore().PeerInfo(peerID)
			if len(peerInfo.Addrs) == 0 {
				fmt.Println("Peer non trovato. Usa prima 'connect'.")
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
				fmt.Println("Identita non caricata.")
				continue
			}
			priv, pub := getOrCreateKey(currentUserIdentity)
			fmt.Printf("\n--- CHIAVI DI %s ---\n", currentUserIdentity)
			fmt.Printf("ID Pubblico: %s\n", pub)
			fmt.Printf("Chiave Privata: %s\n", hex.EncodeToString(priv))
			fmt.Printf("Nonce corrente: %d\n", nonces[pub])
			fmt.Println("-------------------")

		case "exit":
			fmt.Println("Salvataggio in corso...")
			engine.Close()
			fmt.Println("Uscita sicura.")
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