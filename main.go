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
        "log"
	"os"
	"path/filepath"
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
	fmt.Printf("âœ… Nonce inizializzati per %d identitÃ \n", len(nonces))
}

func main() {
	flag.StringVar(&dataDir, "data-dir", "data", "Directory per i dati del nodo")
	p2pPort := flag.Int("port", 4001, "Porta per la connessione P2P")
	flag.Parse()

	fmt.Printf("ðŸ“‚ Directory dati: %s | Porta P2P: %d\n", dataDir, *p2pPort)

	sessionKeys = loadKeysFromDisk(dataDir)

	fmt.Println("==========================================")
	fmt.Println("       PHILIA ECONOMIC PROTOCOL (PEP)     ")
	fmt.Println("          Universal Wallet CLI v1.2 (E2E) ")
	fmt.Println("==========================================")
	fmt.Println("Comandi: create, import, fund, balance, reputation, send, settle,")
	fmt.Println("         send-message, send-agreement, send-escrow, release-escrow,")
	fmt.Println("         dispute-escrow, check-expired, list-messages, chat,")
	fmt.Println("         create-room, list-rooms, connect, sync, anchor, verify, show-key, whoami, exit")
	fmt.Println("==========================================")

	store, err := NewStore(dataDir)
if err != nil {
    log.Fatalf("âŒ Errore apertura database: %v", err)
}
defer store.Close()
	engine := NewEngine(store)
	defer engine.Close()

	initNonces(engine)

	     p2pNode, err := NewP2PNode(context.Background(), *p2pPort, dataDir, engine)
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
			if len(parts) != 3 {
				fmt.Println("Uso: create <nome_utente> <ID_invitante>")
				fmt.Println("âš ï¸ Per unirsi alla rete Ã¨ obbligatorio l'ID pubblico di chi ti invita.")
				continue
			}
			name := parts[1]
			inviterID := parts[2]

			// Verifica preliminare: l'invitante esiste nel DAG?
			// (Una verifica piÃ¹ rigorosa sul suo saldo/limiti Ã¨ giÃ  fatta nel motore quando lui crea l'INVITE)
			if !engine.HasEvent(inviterID) && len(inviterID) < 64 { 
				// Nota: HasEvent cerca l'ID dell'evento, qui facciamo un controllo basilare sulla lunghezza dell'ID pubblico
				fmt.Printf("âš ï¸ Attenzione: l'ID invitante '%s' non sembra valido o non Ã¨ ancora nel DAG locale.\n", inviterID[:16])
				fmt.Println("   Procedi solo se sei certo dell'ID. Sincronizza ('sync') se necessario.")
			}

			priv, id := getOrCreateKey(name)
			_, xPub := getOrCreateX25519Key(name)
			currentUserIdentity = name

			parent := engine.GetLastHash()
			nonce := getNextNonce(id)
			
			// Annunciamo la chiave, il motore ora sa che questa identitÃ  Ã¨ legata a un contesto di rete
			announceEv := NewEvent("KEY_ANNOUNCE", parent, engine.GetAdaptiveParentRefs(), id, inviterID, 0, time.Now().UnixNano(), nonce, 0, hex.EncodeToString(xPub))
			announceEv.Sign(priv)
			
			if err := engine.ProcessEvent(announceEv); err != nil {
				fmt.Printf("âŒ Errore creazione identitÃ : %v\n", err)
				currentUserIdentity = "" // Rollback in caso di errore
				continue
			}

			fmt.Printf("âœ… IdentitÃ  creata: %s\n", name)
			fmt.Printf("   Il tuo ID Pubblico: %s...\n", id[:16])
			fmt.Printf("   Invitante registrato: %s...\n", inviterID[:16])
			fmt.Println("   âœ… Chiave di crittografia E2E annunciata nel DAG")
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
			ev := NewEvent(GENESIS, "", engine.GetAdaptiveParentRefs(), id, "", amount, time.Now().UnixNano(), nonce, 0, "Wallet Funding")
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
				fmt.Println("IdentitÃ  non caricata.")
				continue
			}
			if len(parts) != 4 {
				fmt.Println("Uso: send <ID_destinatario> <importo> <memo>")
				continue
			}
			destID := parts[1]
			amount := parseInt(parts[2])
			memo := parts[3]

			// Anti-spam: limite 50 PAYMENT/giorno
			_, myIDTemp := getOrCreateKey(currentUserIdentity)
			paymentCount := engine.CountEventsByTypeInLast24h(myIDTemp, PAYMENT)
			if paymentCount >= 50 {
				fmt.Printf("âŒ Limite anti-spam raggiunto: %d/50 PAYMENT nelle ultime 24 ore.\n", paymentCount)
				fmt.Println("   Attendi 24 ore o riduci l'attivitÃ .")
				continue
			}

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)
			ttlSeconds := int64(604800)

			ev := NewEvent(PAYMENT, parent, engine.GetAdaptiveParentRefs(), myID, destID, amount, time.Now().UnixNano(), nonce, ttlSeconds, memo)
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

			ev := NewEvent(SETTLE, parent, engine.GetAdaptiveParentRefs(), myID, destID, amount, time.Now().UnixNano(), nonce, 0, "Settlement")
			ev.Sign(priv)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore regolamento: %v\n", err)
			} else {
				fmt.Printf("Pagamento REGOLATO: %d Philia trasferiti a %s...\n", amount, destID[:8])
			}
		case "send-message":
			if currentUserIdentity == "" {
				fmt.Println("IdentitÃ  non caricata.")
				continue
			}
			if len(parts) < 3 {
				fmt.Println("Uso: send-message <ID_destinatario> <testo>")
				continue
			}
			destID := parts[1]
			messageText := strings.Join(parts[2:], " ")
			messageText = strings.Trim(messageText, "\"")

			// Anti-spam: limite 150 MESSAGE/giorno
			_, myIDTemp := getOrCreateKey(currentUserIdentity)
			messageCount := engine.CountEventsByTypeInLast24h(myIDTemp, MESSAGE)
			if messageCount >= 150 {
				fmt.Printf("âŒ Limite anti-spam raggiunto: %d/150 MESSAGE nelle ultime 24 ore.\n", messageCount)
				fmt.Println("   Attendi 24 ore o riduci l'attivitÃ .")
				continue
			}

			priv, myID := getOrCreateKey(currentUserIdentity)
			myXPriv, _ := getOrCreateX25519Key(currentUserIdentity)

			destXPub := engine.GetX25519PubKey(destID)
			if len(destXPub) == 0 {
				fmt.Println("âš ï¸ Chiave X25519 del destinatario non trovata.")
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

			ev := NewEvent(MESSAGE, parent, engine.GetAdaptiveParentRefs(), myID, destID, 0, time.Now().UnixNano(), nonce, 0, hex.EncodeToString(ciphertext))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore invio messaggio: %v\n", err)
			} else {
				fmt.Printf("ðŸ”’ Messaggio E2E inviato a %s...\n", destID[:8])
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

			ev := NewEvent(AGREEMENT, parent, engine.GetAdaptiveParentRefs(), myID, destID, 0, time.Now().UnixNano(), nonce, 0, agreementText)
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("ðŸ“œ Accordo registrato e inviato a %s...\n", destID[:8])
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

			ev := NewEvent(ESCROW_LOCK, parent, engine.GetAdaptiveParentRefs(), myID, sellerID, amount, time.Now().UnixNano(), nonce, 0, string(escrowJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore creazione escrow: %v\n", err)
			} else {
				fmt.Printf("ðŸ”’ ESCROW creato: %s\n", escrowID)
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

			ev := NewEvent(ESCROW_RELEASE, parent, engine.GetAdaptiveParentRefs(), myID, "", 0, time.Now().UnixNano(), nonce, 0, string(releaseJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("âœï¸ Firma ESCROW %s registrata\n", escrowID[:16])
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

			ev := NewEvent(ESCROW_DISPUTE, parent, engine.GetAdaptiveParentRefs(), myID, "", 0, time.Now().UnixNano(), nonce, 0, string(disputeJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("Errore: %v\n", err)
			} else {
				fmt.Printf("âš ï¸ DISPUTA aperta su ESCROW %s\n", escrowID[:16])
			}

		case "check-expired":
			if currentUserIdentity == "" {
				fmt.Println("Identita non caricata.")
				continue
			}
			engine.CheckExpiredPayments()
			fmt.Println("âœ… Controllo pagamenti scaduti completato")

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
			
			fmt.Printf("\nðŸ’¬ Chat con %s... (ultimi %d messaggi)\n", contactID[:16], len(chatEvents))
			fmt.Println("â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€")
			
			if len(chatEvents) == 0 {
				fmt.Println("Nessun messaggio in questa conversazione.")
			} else {
				for _, msg := range chatEvents {
					t := time.Unix(0, msg.Timestamp).Format("15:04")
					direction := "â†’"
					if msg.Sender == myID {
						direction = "â†"
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
			fmt.Println("â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€")

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
			ev := NewEvent(ROOM_CREATE, parent, engine.GetAdaptiveParentRefs(), myID, "", 0, time.Now().UnixNano(), nonce, 0, string(roomJSON))
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
		

		case "room-info":
			if len(parts) != 2 {
				fmt.Println("Uso: room-info <room_id>")
				continue
			}
			roomPrefix := parts[1]
			rooms := engine.GetRooms()

			// Cerca la room per prefisso
			var foundRoom *Room
			var foundID string
			for id, room := range rooms {
				if strings.HasPrefix(id, roomPrefix) {
					foundRoom = room
					foundID = id
					break
				}
			}

			if foundRoom == nil {
				fmt.Printf("âŒ Room con prefisso '%s' non trovata nel DAG locale.\n", roomPrefix)
				continue
			}

			// Ottieni la reputazione del proprietario
			rep := engine.GetReputation(foundRoom.OwnerID)
			visibility := "Pubblica"
			if !foundRoom.IsPublic {
				visibility = "Privata"
			}

			fmt.Println("\nðŸ“‹ === DETTAGLI ROOM ===")
			fmt.Printf("ðŸª Nome:        %s\n", foundRoom.Name)
			fmt.Printf("ðŸ“‚ Categoria:   %s\n", foundRoom.Category)
			fmt.Printf("ðŸ’° Prezzo:      %d Philia\n", foundRoom.BasePrice)
			fmt.Printf("ðŸ‘ï¸  VisibilitÃ :  %s\n", visibility)
			fmt.Printf("ðŸ‘¤ Proprietario: %s...\n", foundRoom.OwnerID[:16])
			fmt.Printf("â­ Reputazione:  %d (Livello: %s)\n", rep.Score, rep.TrustLevel)
			fmt.Printf("ðŸ”‘ ID Completo: %s\n", foundID)
			fmt.Println("=======================\n")

		case "buy-escrow":
			if currentUserIdentity == "" {
				fmt.Println("âš ï¸ IdentitÃ  non caricata. Usa 'create' o 'import' prima.")
				continue
			}
			if len(parts) != 2 {
				fmt.Println("Uso: buy-escrow <room_id>")
				continue
			}
			roomPrefix := parts[1]
			rooms := engine.GetRooms()
			
			// 1. Trova la room per prefisso
			var foundRoom *Room
			var foundID string
			for id, room := range rooms {
				if strings.HasPrefix(id, roomPrefix) {
					foundRoom = room
					foundID = id
					break
				}
			}
			
			if foundRoom == nil {
				fmt.Printf("âŒ Room con prefisso '%s' non trovata.\n", roomPrefix)
				continue
			}
			
			// 2. Estrai automaticamente venditore e prezzo dalla Room
			sellerID := foundRoom.OwnerID
			amount := foundRoom.BasePrice
			
			if amount <= 0 {
				fmt.Println("âŒ Prezzo della room non valido.")
				continue
			}

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			// 3. Crea l'oggetto Escrow collegato alla Room
			escrowID := fmt.Sprintf("esc_%s_%d", myID[:8], time.Now().Unix())
			escrow := Escrow{
				ID: escrowID, Buyer: myID, Seller: sellerID,
				Arbitrator: "0000000000000000000000000000000000000000000000000000000000000000", 
				Amount: amount, Description: fmt.Sprintf("Acquisto Room: %s (ID: %s)", foundRoom.Name, foundID), RequiredSigs: 2,
				Signatures: make(map[string]string),
				CreatedAt: time.Now().UnixNano(),
				ExpiresAt: time.Now().Add(7 * 24 * time.Hour).UnixNano(),
			}
			escrowJSON, _ := json.Marshal(escrow)

			// 4. Crea e firma l'evento ESCROW_LOCK
			ev := NewEvent(ESCROW_LOCK, parent, engine.GetAdaptiveParentRefs(), myID, sellerID, amount, time.Now().UnixNano(), nonce, 0, string(escrowJSON))
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("âŒ Errore creazione escrow: %v\n", err)
			} else {
				fmt.Printf("ðŸ”’ ACQUISTO SICURO INIZIATO: %d Philia bloccati in Escrow per %s...\n", amount, sellerID[:8])
				fmt.Printf("   ðŸ“Ž Riferimento Room: %s (%s)\n", foundRoom.Name, foundID[:16])
				fmt.Printf("   ðŸ’¡ Prossimo passo: il venditore usa 'release-escrow %s' per sbloccare i fondi.\n", escrowID)
			}
		case "buy":
			if currentUserIdentity == "" {
				fmt.Println("âš ï¸ IdentitÃ  non caricata. Usa 'create' o 'import' prima.")
				continue
			}
			if len(parts) != 4 {
				fmt.Println("Uso: buy <ID_venditore> <importo> <room_id>")
				continue
			}
			sellerID := parts[1]
			amount := parseInt(parts[2])
			roomID := parts[3]

			if amount <= 0 {
				fmt.Println("L'importo deve essere positivo.")
				continue
			}

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)
			
			// Il memo lega esplicitamente il pagamento alla Room, creando il "ponte"
			memo := fmt.Sprintf("Acquisto in Room: %s", roomID)

			ev := NewEvent(PAYMENT, parent, engine.GetAdaptiveParentRefs(), myID, sellerID, amount, time.Now().UnixNano(), nonce, 0, memo)
			ev.Sign(priv)

			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("âŒ Errore acquisto: %v\n", err)
			} else {
				fmt.Printf("ðŸ›’ ACQUISTO INIZIATO: %d Philia prenotati per %s...\n", amount, sellerID[:8])
				   displayRoomID := roomID
   if len(roomID) > 16 {
       displayRoomID = roomID[:16]
   }
   fmt.Printf("   ðŸ“Ž Riferimento Room: %s\n", displayRoomID)
				fmt.Printf("   ðŸ’¡ Prossimo passo: il venditore usa 'settle %s %d' per completare.\n", sellerID, amount)
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
				fmt.Printf("âœ… Connesso al peer: %s\n", peerInfo.ID)
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

		case "whoami":
			if currentUserIdentity == "" {
				fmt.Println("⚠️ Identità non caricata. Usa 'create' o 'import' prima.")
				continue
			}
			
			_, pubKey := getOrCreateKey(currentUserIdentity)
			peerID := p2pNode.Host.ID().String()
			
			fmt.Println("\n👤 === LE TUE CREDENZIALI ===")
			fmt.Printf("📛 Nome identità:      %s\n", currentUserIdentity)
			fmt.Printf("🔑 ID Pubblico:        %s\n", pubKey)
			fmt.Printf("   (Usalo per: send, send-message, send-agreement)\n")
			fmt.Printf("🌐 PeerID (Rete):      %s\n", peerID)
			fmt.Printf("   (Usalo per: connect, sync)\n")
			fmt.Println("===========================\n")

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

						case "status":
			if currentUserIdentity == "" {
				fmt.Println("⚠️ Identità non caricata. Usa 'create' o 'import' prima.")
				continue
			}

			// FIX: Risolviamo il nome dell'identità nella sua Chiave Pubblica
			_, pubKey := getOrCreateKey(currentUserIdentity)
			
			// 🔍 DEBUG: Stampiamo cosa stiamo cercando
			

			// FIX CRUCIALE: Usiamo pubKey qui
			total, orphans, bal, res, rep, trust := engine.GetStatus(pubKey)
			
			// 🔍 DEBUG: Stampiamo cosa ci ha risposto il motore
			

			// Calcola lo spazio disco
			dirSize, _ := getDirSize(dataDir)
			dirSizeStr := formatBytes(dirSize)

			fmt.Println("\n📊 === STATO NODO PEP ===")
			fmt.Printf("🔹 Eventi nel DAG:      %d\n", total)
			fmt.Printf("🔹 Eventi orfani:       %d\n", orphans)
			fmt.Printf("🔹 Spazio disco (data): %s\n", dirSizeStr)
			fmt.Println("------------------------")
			fmt.Printf("👤 Identità:            %s\n", currentUserIdentity)
			fmt.Printf("💰 Saldo disponibile:   %d Philia\n", bal)
			fmt.Printf("🔒 Saldo prenotato:     %d Philia\n", res)
			fmt.Printf("⭐ Reputazione:         %d (Livello: %s)\n", rep, trust)
			fmt.Println("========================\n")

				case "invite":
			if currentUserIdentity == "" {
				fmt.Println("âš ï¸ IdentitÃ  non caricata. Usa 'create' o 'import' prima.")
				continue
			}
			if len(parts) != 2 {
				fmt.Println("Uso: invite <ID_destinatario>")
				continue
			}
			destID := parts[1]

			priv, myID := getOrCreateKey(currentUserIdentity)
			parent := engine.GetLastHash()
			nonce := getNextNonce(myID)

			// Otteniamo le statistiche solo per costruire l'evento e per il feedback utente.
			// La validazione reale (saldo e limiti) avviene in modo atomico e sicuro dentro engine.ProcessEvent.
			currentInvites, maxAllowed, nextCost, currentScore := engine.GetInviteStats(myID)

			// Creiamo l'evento. Il motore verificherÃ  e applicherÃ  il costo reale in modo blindato.
			ev := NewEvent(INVITE, parent, engine.GetAdaptiveParentRefs(), myID, destID, nextCost, time.Now().UnixNano(), nonce, 0, fmt.Sprintf("cost:%d", nextCost))
			ev.Sign(priv)

			// Delegiamo la validazione economica e anti-spam al motore (Single Source of Truth)
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("âŒ Errore invito: %v\n", err)
			} else {
				fmt.Printf("âœ… Invito inviato a %s!\n", destID[:16])
				fmt.Printf("   Costo applicato: %d Philia | Inviti usati: %d/%d | Score: %d\n", nextCost, currentInvites+1, maxAllowed, currentScore)
				fmt.Println("   âš ï¸ Ricorda: la responsabilitÃ  solidale Ã¨ attiva.")
			}

		case "debug-dag":
			fmt.Println("\n🔍 ULTIMI EVENTI NEL DAG:")
			log := engine.GetEventLog()
			limit := 3
			if len(log) < limit {
				limit = len(log)
			}
			for i := len(log) - 1; i >= len(log)-limit; i-- {
				ev := log[i]
				// Mostriamo solo i primi 8 caratteri dell'ID per leggibilità
				fmt.Printf("ID: %s... | Tipo: %-7s | ParentRefs: %v\n", ev.ID[:8], ev.Type, ev.ParentRefs)
			}
			fmt.Println()

		case "debug-orphan":
			fmt.Println("\n🕳️ SIMULAZIONE ORFANO: Creazione di un evento con genitore inesistente...")
			if currentUserIdentity == "" {
				fmt.Println("⚠️ Identità non caricata. Usa 'create' prima.")
				continue
			}
			priv, myID := getOrCreateKey(currentUserIdentity)
			nonce := getNextNonce(myID)
			
			// Creiamo un hash fasullo di 64 caratteri (64 zeri)
			fakeParent := "0000000000000000000000000000000000000000000000000000000000000000"
			
			// Creiamo l'evento forzando il ParentHash fasullo
			ev := NewEvent(MESSAGE, fakeParent, engine.GetAdaptiveParentRefs(), myID, myID, 0, time.Now().UnixNano(), nonce, 0, "Test Orfano Intenzionale")
			ev.Sign(priv)

			// Proviamo a processarlo
			if err := engine.ProcessEvent(ev); err != nil {
				fmt.Printf("❌ Errore inaspettato: %v\n", err)
			} else {
				fmt.Println("✅ Evento inviato al motore. Controlla se è finito nell'orphanPool con il comando 'status'.")
			}

		case "debug-test-ttl":
			fmt.Println("\n⏳ TEST SELF-HEALING TTL INIZIATO...")
			if currentUserIdentity == "" {
				fmt.Println("⚠️ Identità non caricata. Usa 'create' prima.")
				continue
			}
			priv, myID := getOrCreateKey(currentUserIdentity)

			// 1. Finanziamento iniziale di 1000 Philia
			fundEv := NewEvent(GENESIS, "", engine.GetAdaptiveParentRefs(), myID, "", 1000, time.Now().UnixNano(), getNextNonce(myID), 0, "Test Fund")
			fundEv.Sign(priv)
			engine.ProcessEvent(fundEv)
			
			bal, res := engine.GetBalance(myID)
			fmt.Printf("💰 1. Dopo FUND:         Disponibile=%4d | Prenotato=%4d\n", bal, res)

			// 2. Invio di 500 Philia con TTL di 5 secondi a un destinatario fittizio
			fakeDest := "0000000000000000000000000000000000000000000000000000000000000000"
			payEv := NewEvent(PAYMENT, engine.GetLastHash(), engine.GetAdaptiveParentRefs(), myID, fakeDest, 500, time.Now().UnixNano(), getNextNonce(myID), 5, "Test TTL Scadenza Rapida")
			payEv.Sign(priv)
			engine.ProcessEvent(payEv)
			
			bal, res = engine.GetBalance(myID)
			fmt.Printf("🔒 2. Dopo PAYMENT (5s): Disponibile=%4d | Prenotato=%4d\n", bal, res)

			// 3. Attesa della scadenza
			fmt.Println("⏱️ 3. Attendo 6 secondi per la scadenza automatica...")
			time.Sleep(6 * time.Second)

			// 4. Forziamo il controllo delle scadenze e mostriamo il risultato
			engine.CheckExpiredPayments()
			bal, res = engine.GetBalance(myID)
			fmt.Printf("✅ 4. Dopo EXPIRED:      Disponibile=%4d | Prenotato=%4d\n", bal, res)
			
			if bal == 1000 && res == 0 {
				fmt.Println("🎉 SUCCESSO: I fondi sono tornati atomicamente nel saldo disponibile! Self-Healing attivo.")
			} else {
				fmt.Println("❌ ERRORE: I fondi non sono tornati correttamente. Controllare la logica di sblocco.")
			}

				case "debug-simulate-p2p":
			fmt.Println("\n🔄 SIMULAZIONE RISOLUZIONE ORFANI P2P (Consegna Fuori Ordine)...")
			
			// Usiamo due identità distinte per avere sequenze di nonce indipendenti
			privA, idA := getOrCreateKey("alice_test")
			privB, idB := getOrCreateKey("bob_test")
			
			// 1. Creiamo Evento A (Genitore) da Alice
			nonceA := getNextNonce(idA)
			evA := NewEvent(MESSAGE, engine.GetLastHash(), engine.GetAdaptiveParentRefs(), idA, idB, 0, time.Now().UnixNano(), nonceA, 0, "Sono il Genitore A (da Alice)")
			evA.Sign(privA)
			
			// 2. Creiamo Evento B (Figlio) da Bob, che punta ad A
			nonceB := getNextNonce(idB)
			evB := NewEvent(MESSAGE, evA.ID, []string{}, idB, idA, 0, time.Now().UnixNano(), nonceB, 0, "Sono il Figlio B (da Bob, punta ad A)")
			evB.Sign(privB)
			
			fmt.Printf("📦 Evento A generato (ID: %s...) da Alice - NON ANCORA NEL MOTORE\n", evA.ID[:8])
			fmt.Printf("📦 Evento B generato (ID: %s...) da Bob - Punta a A come genitore\n", evB.ID[:8])
			
			// 3. Inviamo prima il Figlio (B). In P2P, i pacchetti arrivano fuori ordine.
			fmt.Println("\n1️⃣ Invio Evento B (Figlio) al motore per primo...")
			engine.ProcessEvent(evB)
			
			total, orphans, _, _, _, _ := engine.GetStatus(idA)
			fmt.Printf("   👉 Stato Motore: Eventi nel DAG=%d, Orfani=%d\n", total, orphans)
			if orphans == 0 {
				fmt.Println("   ❌ FALLIMENTO: B avrebbe dovuto essere classificato come orfano!")
				continue
			}
			
			// 4. Ora inviamo il Genitore (A). Questo dovrebbe innescare processOrphans() e sbloccare B.
			fmt.Println("\n2️⃣ Invio Evento A (Genitore) al motore...")
			engine.ProcessEvent(evA)
			
			total, orphans, _, _, _, _ = engine.GetStatus(idA)
			fmt.Printf("   👉 Stato Motore: Eventi nel DAG=%d, Orfani=%d\n", total, orphans)
			
			if orphans == 0 && total >= 2 {
				fmt.Println("🎉 SUCCESSO ASSOLUTO: L'arrivo del genitore ha sbloccato automaticamente l'orfano! (Effetto a cascata attivo)")
			} else {
				fmt.Println("❌ FALLIMENTO: L'orfano non è stato sbloccato. La logica processOrphans() ha un difetto.")
			}

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

