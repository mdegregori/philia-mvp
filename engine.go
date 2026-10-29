package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
        "os"
)

// --- STRUTTURA ENGINE ---

type Engine struct {
	x25519Registry map[string][]byte // Mappa: Ed25519 PubKey -> X25519 PubKey
	store          *Store
	balances       map[string]int64        // SALDO CONTABILE (Ledger)
	reserved       map[string]int64        // PARTITE PRENOTATE (Reserved)
	nonces         map[string]uint64       // Anti-replay: ultimo nonce visto per identità
	reputation     map[string]int          // Punteggio reputazione (calcolato on-DAG)
        genesisIDs     map[string]bool         //Identità fondatrici con reputazione iniziale (Bootstrap)
	rooms          map[string]*Room
	escrows        map[string]*Escrow      // Contratti multi-firma attivi
	eventLog       []Event
	orphanPool     map[string]Event
	messages       map[string][]Event
	ackReceipts    map[string][]Event      // Mappa: OriginalMsgID -> Lista di ACK ricevuti
	agreements     map[string][]Event
	mu             sync.RWMutex
}

// --- COSTRUZIONE ---

func NewEngine(store *Store) *Engine {
	engine := &Engine{
		store:          store,
		balances:       make(map[string]int64),
		reserved:       make(map[string]int64),
		nonces:         make(map[string]uint64),
                genesisIDs:     make(map[string]bool),  //Inizializzazione mappa Genesis
		reputation:     make(map[string]int),
		rooms:          make(map[string]*Room),
		escrows:        make(map[string]*Escrow),
		eventLog:       make([]Event, 0),
		orphanPool:     make(map[string]Event),
		messages:       make(map[string][]Event),
		ackReceipts:    make(map[string][]Event),
		agreements:     make(map[string][]Event),
		x25519Registry: make(map[string][]byte),
	}

        engine.loadGenesisIDs()

	events, err := store.LoadEvents()
	if err != nil {
		fmt.Println("Nessun evento precedente trovato. Avvio da zero.")
		return engine
	}

	fmt.Printf("🔄 Caricamento di %d eventi dal disco...\n", len(events))
	for _, ev := range events {
		engine.applyEventInternal(ev)
		engine.eventLog = append(engine.eventLog, ev)
		// Ricostruisci i nonce dal log
		if ev.Nonce > engine.nonces[ev.Sender] {
			engine.nonces[ev.Sender] = ev.Nonce
		}
	}
	fmt.Printf("✅ Avvio completato: %d eventi nel DAG, %d identità attive, %d orfani in attesa.\n", len(engine.eventLog), len(engine.nonces), len(engine.orphanPool))
	return engine
}

// loadGenesisIDs carica le identità fondatrici da genesis.json
// Se il file non esiste, ne crea uno di default per permettere i test immediati.
func (e *Engine) loadGenesisIDs() {
	data, err := os.ReadFile("genesis.json")
	if err != nil {
		// File non trovato: creiamo un default con l'ID di Alice per i test
		defaultGenesis := `{"founders": ["a1c4b1528432cea1dacd1131f4ba6ce8fb23cc06dff6ecfc7ee79041eec2616c"]}`
		os.WriteFile("genesis.json", []byte(defaultGenesis), 0644)
		data = []byte(defaultGenesis)
	}

	var genesis struct {
		Founders []string `json:"founders"`
	}
	if err := json.Unmarshal(data, &genesis); err == nil {
		for _, id := range genesis.Founders {
			e.genesisIDs[id] = true
		}
		fmt.Printf("🌱 Bootstrap: caricate %d identità Genesis dal file.\n", len(genesis.Founders))
	} else {
		fmt.Printf("⚠️ Errore nel parsing di genesis.json: %v\n", err)
	}
}

// --- PROCESSAMENTO EVENTI ---

func (e *Engine) ProcessEvent(ev Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 1. Validazione strutturale
	if err := ev.ValidateBasic(); err != nil {
		return err
	}

	// 2. Verifica firma crittografica (Ed25519)
	if err := ev.Verify(); err != nil {
		return fmt.Errorf("firma non valida: %v", err)
	}

	// 3. Anti-replay: verifica nonce monotono
	if err := e.validateNonce(ev); err != nil {
		return err
	}

	// 4. Idempotenza: skip duplicati
	if e.HasEvent(ev.ID) {
		return nil
	}

	// 5. Verifica causalità (parent exists)
	// FIX: Permettiamo parent vuoto per GENESIS e KEY_ANNOUNCE (primi eventi)
	if ev.Type != GENESIS && ev.Type != "KEY_ANNOUNCE" {
		if !e.parentExists(ev.ParentHash) {
			fmt.Printf("⏳ Evento %s è ORFANO (attesa genitore %s)\n", ev.ID[:8], ev.ParentHash[:8])
			e.orphanPool[ev.ID] = ev
			return nil
		}
	}

	// 6. Registra la chiave pubblica X25519 se è un annuncio
	if ev.Type == "KEY_ANNOUNCE" {
		keyBytes, err := hex.DecodeString(ev.Memo)
		if err == nil {
			e.x25519Registry[ev.Sender] = keyBytes
		}
	}

	// 7. Verifica double spend (se PAYMENT)
	if ev.Type == PAYMENT {
		e.checkDoubleSpend(ev)
	}
	// 7.5. Politica Economica: Validazione rigorosa degli INVITE
	if ev.Type == INVITE {
		// A. Conta quanti inviti ha già fatto questo sender
		inviteCount := 0
		for _, e := range e.eventLog {
			if e.Type == INVITE && e.Sender == ev.Sender {
				inviteCount++
			}
		}

		// B. Calcola il limite massimo (max 5, o Score/20)
		// Usiamo e.reputation[ev.Sender] per evitare chiamate a funzioni che potrebbero lockare
		baseRep := e.reputation[ev.Sender]
		maxAllowed := 5
		if baseRep > 0 {
			calc := (baseRep + 19) / 20 // Arrotondamento per eccesso
			if calc > 5 {
				maxAllowed = calc
			}
		}

		if inviteCount >= maxAllowed {
			return fmt.Errorf("politica violata: limite inviti raggiunto (%d/%d) per %s", inviteCount, maxAllowed, ev.Sender[:8])
		}

		// C. Calcola il costo progressivo
		var cost int64 = 1
		if inviteCount >= 10 {
			cost = 25
		} else if inviteCount >= 5 {
			cost = 5
		}

		// D. Verifica e brucia il saldo (atomico, siamo già sotto Lock)
		if e.balances[ev.Sender] < cost {
			return fmt.Errorf("politica violata: saldo insufficiente per fee invito (servono %d, hai %d)", cost, e.balances[ev.Sender])
		}
		
		e.balances[ev.Sender] -= cost
		fmt.Printf("🔥 [MOTORE] Fee invito di %d Philia bruciata da %s\n", cost, ev.Sender[:8])
	}

		// 7.6. Politica Economica: Validazione Anti-Spam Strict
	if ev.Type == PAYMENT || ev.Type == MESSAGE {
		now := time.Now().UnixNano()
		cutoff := now - (24 * 60 * 60 * 1e9)
		count := 0
		for _, logEv := range e.eventLog {
			if logEv.Sender == ev.Sender && logEv.Type == ev.Type && logEv.Timestamp >= cutoff {
				count++
			}
		}

		var limit int
		if ev.Type == PAYMENT {
			limit = 50
		} else { // MESSAGE
			limit = 150
		}

		if count >= limit {
			// RIFIUTO SECCO: L'evento non entra nel DAG.
			// Nessun slashing all'invitante: penalizzare un tentativo bloccato 
			// è un vettore di attacco Sybil (griefing) contro l'invitante.
			// La responsabilità solidale si applica solo a violazioni *confermate* nel DAG.
			return fmt.Errorf("politica violata: limite anti-spam raggiunto (%d/%d %s)", count, limit, ev.Type)
		}
	}

	// 8. Applica evento
	if err := e.applyEventInternal(ev); err != nil {
		return err
	}

	// 9. Persisti (append-only)
	if err := e.store.AppendEvent(ev); err != nil {
		return fmt.Errorf("errore salvataggio: %w", err)
	}

	// 10. Aggiungi al log in memoria
	e.eventLog = append(e.eventLog, ev)

       // 11. EFFETTTO A CASCATA: Controlla se questonuovo evento sblocca degli orfani
       e.processOrphans()

	return nil
}
// InitX25519Registry ricostruisce la mappa delle chiavi pubbliche X25519 all'avvio del nodo
func (e *Engine) InitX25519Registry() {
	count := 0
	for _, ev := range e.GetEventLog() {
		if ev.Type == "KEY_ANNOUNCE" {
			keyBytes, err := hex.DecodeString(ev.Memo)
			if err == nil {
				e.x25519Registry[ev.Sender] = keyBytes
				count++
			}
		}
	}
	fmt.Printf("✅ Registro chiavi X25519 ricostruito: %d chiavi caricate dal DAG\n", count)
}

// --- ORPHAN POOL ---

func (e *Engine) processOrphans() {
	for {
		processed := false
		for id, ev := range e.orphanPool {
			// 1. Verifica che TUTTI i genitori dichiarati esistano (ParentHash + ParentRefs)
			allParentsExist := true
			
			if ev.ParentHash != "" && !e.parentExists(ev.ParentHash) {
				allParentsExist = false
			}
			
			if allParentsExist {
				for _, ref := range ev.ParentRefs {
					if ref != "" && !e.parentExists(ref) {
						allParentsExist = false
						break
					}
				}
			}

			// 2. Se tutti i genitori ci sono, promuovi l'orfano
			if allParentsExist {
				if err := e.validateNonce(ev); err != nil {
					fmt.Printf("⚠️ Orfano %s rigettato (nonce): %v\n", id[:8], err)
					delete(e.orphanPool, id)
					continue
				}
				if err := e.applyEventInternal(ev); err != nil {
					fmt.Printf("⚠️ Errore applicazione evento orfano %s: %v\n", id[:8], err)
					delete(e.orphanPool, id)
					continue
				}
				if err := e.store.AppendEvent(ev); err != nil {
					fmt.Printf("⚠️ Errore salvataggio evento orfano %s: %v\n", id[:8], err)
					delete(e.orphanPool, id)
					continue
				}
				e.eventLog = append(e.eventLog, ev)
				delete(e.orphanPool, id)
				processed = true // Continua il ciclo per vedere se questo sblocca altri orfani
			}
		}
		if !processed {
			break // Nessun orfano è stato processato in questo giro, esci dal ciclo
		}
	}
	if len(e.orphanPool) > 0 {
		fmt.Printf("⚠️ %d eventi ancora orfani (genitori mancanti)\n", len(e.orphanPool))
	}
}

// --- APPLICAZIONE EVENTI (CORE) ---

func (e *Engine) applyEventInternal(ev Event) error {
	// 🔒 COMMIT DEL NONCE: Avviene solo ora che l'evento è stato validato e sta per essere applicato.
	// Questo risolve il bug per cui gli orfani venivano rigettati come "replay" al secondo tentativo.
	e.nonces[ev.Sender] = ev.Nonce

	switch ev.Type {
			case GENESIS:
			
			e.balances[ev.Sender] += ev.Amount
			
	case "KEY_ANNOUNCE":
		ev.Status = "CONFIRMED"

	case PAYMENT:
		if ev.IsExpired() {
			fmt.Printf("PAYMENT %s SCADUTO (TTL superato), fondi sbloccati\n", ev.ID[:8])
			ev.Status = "EXPIRED"
			return nil
		}
		available := e.balances[ev.Sender] - e.reserved[ev.Sender]
		if available < ev.Amount {
			return fmt.Errorf("saldo disponibile insufficiente: %d < %d", available, ev.Amount)
		}
		e.reserved[ev.Sender] += ev.Amount
		ev.Status = "PENDING"
		fmt.Printf("PAYMENT: Prenotati %d da %s (TTL: %ds)\n", ev.Amount, ev.Sender[:8], ev.TTLSeconds)

	case SETTLE:
		if e.reserved[ev.Sender] < ev.Amount {
			return errors.New("partita prenotata insufficiente")
		}
		e.reserved[ev.Sender] -= ev.Amount
		e.balances[ev.Sender] -= ev.Amount
		e.balances[ev.Recipient] += ev.Amount
		
		ev.Status = "SETTLED"
		fmt.Printf("SETTLE: Trasferiti %d da %s a %s | Reputazione %s: +10\n",
			ev.Amount, ev.Sender[:8], ev.Recipient[:8], ev.Recipient[:8])

	case ROOM_CREATE:
		var room Room
		if err := json.Unmarshal([]byte(ev.Memo), &room); err != nil {
			return fmt.Errorf("dati room non validi: %v", err)
		}
		room.ID = ev.ID
		room.OwnerID = ev.Sender
		room.CreatedAt = ev.Timestamp
		e.rooms[room.ID] = &room
		fmt.Printf("🏪 ROOM creata: '%s' (ID: %s...) da %s\n", room.Name, room.ID[:8], ev.Sender[:8])

	case ROOM_UPDATE:
		var update struct {
			RoomID     string `json:"room_id"`
			NewName    string `json:"new_name,omitempty"`
			NewPrice   int64  `json:"new_price,omitempty"`
			NewDesc    string `json:"new_description,omitempty"`
			SetPrivate *bool  `json:"set_private,omitempty"`
		}
		if err := json.Unmarshal([]byte(ev.Memo), &update); err != nil {
			return fmt.Errorf("dati aggiornamento non validi: %v", err)
		}
		room, exists := e.rooms[update.RoomID]
		if !exists {
			return errors.New("room non esistente")
		}
		if room.OwnerID != ev.Sender {
			return errors.New("NON AUTORIZZATO: solo il proprietario può modificare questa room")
		}
		if update.NewName != "" {
			room.Name = update.NewName
		}
		if update.NewPrice > 0 {
			room.BasePrice = update.NewPrice
		}
		if update.NewDesc != "" {
			room.Description = update.NewDesc
		}
		if update.SetPrivate != nil {
			room.IsPublic = !*update.SetPrivate
		}
		fmt.Printf("ROOM aggiornata: '%s' (ID: %s...)\n", room.Name, room.ID[:8])

	case MESSAGE:
		e.messages[ev.Recipient] = append(e.messages[ev.Recipient], ev)
		fmt.Printf("💬 Messaggio da %s a %s: %s\n", ev.Sender[:8], ev.Recipient[:8], ev.Memo)

	case MESSAGE_ACK:
		var ack MessageACK
		if err := json.Unmarshal([]byte(ev.Memo), &ack); err != nil {
			return fmt.Errorf("dati MESSAGE_ACK non validi: %v", err)
		}

		// Verifica la firma dell'ACK contenuta nel memo
		if err := verifyACKSignature(ack); err != nil {
			return fmt.Errorf("ricevuta ACK non valida: %w", err)
		}

		// Registra l'ACK nella mappa dell'Engine
		e.ackReceipts[ack.OriginalMsgID] = append(e.ackReceipts[ack.OriginalMsgID], ev)

		// Premia la correttezza del destinatario con +1 di reputazione
		e.reputation[ev.Sender] += 1
		ev.Status = "CONFIRMED"

		fmt.Printf("📜 ACK CERTIFICATO registrato per messaggio %s da %s\n",
			ack.OriginalMsgID[:8], ev.Sender[:8])

	case AGREEMENT:
		e.agreements[ev.Recipient] = append(e.agreements[ev.Recipient], ev)
		fmt.Printf("📜 Accordo registrato da %s a %s: %s\n", ev.Sender[:8], ev.Recipient[:8], ev.Memo)

	// --- ESCROW MULTI-FIRMA ---
	case ESCROW_LOCK:
		var escrow Escrow
		if err := json.Unmarshal([]byte(ev.Memo), &escrow); err != nil {
			return fmt.Errorf("dati escrow non validi: %v", err)
		}
		available := e.balances[ev.Sender] - e.reserved[ev.Sender]
		if available < escrow.Amount {
			return errors.New("fondi insufficienti per escrow")
		}
		e.reserved[ev.Sender] += escrow.Amount
		escrow.Status = "LOCKED"
		escrow.Signatures = make(map[string]string)
		e.escrows[escrow.ID] = &escrow
		ev.Status = "PENDING"
		fmt.Printf("🔒 ESCROW creato: %s | Importo: %d | Richieste: %d firme\n",
			escrow.ID[:8], escrow.Amount, escrow.RequiredSigs)

	case ESCROW_RELEASE:
		var release struct {
			EscrowID string `json:"escrow_id"`
		}
		if err := json.Unmarshal([]byte(ev.Memo), &release); err != nil {
			return fmt.Errorf("dati release non validi: %v", err)
		}
		escrow, exists := e.escrows[release.EscrowID]
		if !exists {
			return errors.New("escrow non esistente")
		}
		if escrow.Status != "LOCKED" && escrow.Status != "DISPUTED" {
			return fmt.Errorf("escrow non in stato valido: %s", escrow.Status)
		}
		escrow.Signatures[ev.Sender] = ev.Signature
		fmt.Printf("✍️ Firma ESCROW %s da %s (%d/%d)\n",
			escrow.ID[:8], ev.Sender[:8], len(escrow.Signatures), escrow.RequiredSigs)

		if len(escrow.Signatures) >= escrow.RequiredSigs {
			e.reserved[escrow.Buyer] -= escrow.Amount
			e.balances[escrow.Buyer] -= escrow.Amount
			e.balances[escrow.Seller] += escrow.Amount
			escrow.Status = "RELEASED"
			
			ev.Status = "SETTLED"
			fmt.Printf("✅ ESCROW %s RILASCIATO: %d Philia a %s\n",
				escrow.ID[:8], escrow.Amount, escrow.Seller[:8])
		}

	case ESCROW_DISPUTE:
		var dispute struct {
			EscrowID    string `json:"escrow_id"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal([]byte(ev.Memo), &dispute); err != nil {
			return fmt.Errorf("dati disputa non validi: %v", err)
		}
		escrow, exists := e.escrows[dispute.EscrowID]
		if !exists {
			return errors.New("escrow non esistente")
		}
		escrow.Status = "DISPUTED"
		escrow.Description += " [DISPUTA: " + dispute.Description + "]"
		ev.Status = "PENDING"
		fmt.Printf("⚠️ DISPUTA aperta su ESCROW %s: %s\n", escrow.ID[:8], dispute.Description)

	case INVITE:
		// Brucia la fee di invito dal saldo del mittente
		if e.balances[ev.Sender] >= ev.Amount {
			e.balances[ev.Sender] -= ev.Amount
			fmt.Printf("🔥 INVITO: bruciati %d Philia da %s\n", ev.Amount, ev.Sender[:8])
		} else {
			return fmt.Errorf("saldo insufficiente per bruciare la fee di invito")
		}

	default:
		return fmt.Errorf("tipo evento sconosciuto: %s", ev.Type)

	
	}

	return nil
}

// Helper interno per verificare la firma di un MessageACK
func verifyACKSignature(ack MessageACK) error {
	if ack.OriginalMsgID == "" {
		return errors.New("original_msg_id mancante nell'ACK")
	}
	pubKeyBytes, err := hex.DecodeString(ack.RecipientID)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("chiave pubblica recipient non valida: %v", err)
	}
	sigBytes, err := hex.DecodeString(ack.Signature)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("formato firma non valido: %v", err)
	}

	payload := fmt.Sprintf("%s|%s|%d", ack.OriginalMsgID, ack.RecipientID, ack.Timestamp)
	hash := sha256.Sum256([]byte(payload))

	if !ed25519.Verify(pubKeyBytes, hash[:], sigBytes) {
		return errors.New("firma della ricevuta non valida")
	}
	return nil
}

// --- CONSENSO CLO (CAUSAL LEXICOGRAPHIC ORDERING) ---

func (e *Engine) resolveConflict(a, b Event) Event {
	if a.Nonce != b.Nonce {
		if a.Nonce < b.Nonce {
			return a
		}
		return b
	}
	if a.ID < b.ID {
		return a
	}
	return b
}

func (e *Engine) checkDoubleSpend(ev Event) {
	available := e.balances[ev.Sender] - e.reserved[ev.Sender]

	if available >= ev.Amount {
		return
	}

	for _, oldEv := range e.eventLog {
		if oldEv.Type != PAYMENT {
			continue
		}
		if oldEv.Sender != ev.Sender {
			continue
		}
		if oldEv.Status == "SETTLED" || oldEv.Status == "EXPIRED" || oldEv.Status == "INVALID" {
			continue
		}
		if oldEv.ParentHash != ev.ParentHash && oldEv.ID != ev.ParentHash && ev.ID != oldEv.ParentHash {
			winner := e.resolveConflict(ev, oldEv)
			if winner.ID == ev.ID {
				oldEv.Status = "INVALID"
				e.reserved[oldEv.Sender] -= oldEv.Amount
				fmt.Printf("⚔️ CLO: vince nuovo evento %s (nonce %d), invalidato %s\n",
					ev.ID[:8], ev.Nonce, oldEv.ID[:8])
			} else {
				ev.Status = "INVALID"
				fmt.Printf("⚔️ CLO: vince evento esistente %s (nonce %d), rigettato %s\n",
					oldEv.ID[:8], oldEv.Nonce, ev.ID[:8])
				return
			}
		}
	}
}

// --- ANTI-REPLAY ---

func (e *Engine) validateNonce(ev Event) error {
	lastSeen := e.nonces[ev.Sender]
	if ev.Nonce <= lastSeen {
		return fmt.Errorf("REPLAY ATTACK: nonce %d già usato (ultimo visto: %d)", ev.Nonce, lastSeen)
	}
	
	// Opzionale ma consigliato: impedisce salti di nonce troppo ampi (anti-spam)
	if ev.Nonce > lastSeen+10 { 
		return fmt.Errorf("NONCE GAP: atteso circa %d, ricevuto %d", lastSeen+1, ev.Nonce)
	}
	
	// 🔥 FIX CRUCIALE: NON aggiorniamo e.nonces qui. 
	// Lo faremo solo in applyEventInternal, quando l'evento è davvero applicato.
	return nil
}

// --- TTL & SCADENZA ---

func (e *Engine) CheckExpiredPayments() {
	now := time.Now().UnixNano()
	for i := range e.eventLog {
		ev := &e.eventLog[i]
		if ev.Type != PAYMENT {
			continue
		}
		if ev.Status != "PENDING" {
			continue
		}
		if ev.ExpiresAt > 0 && now > ev.ExpiresAt {
			e.reserved[ev.Sender] -= ev.Amount
			ev.Status = "EXPIRED"
			
			// 🔒 PERSISTENZA: Salva lo stato aggiornato nel database
			if err := e.store.AppendEvent(*ev); err != nil {
				fmt.Printf("❌ Errore salvataggio stato EXPIRED: %v\n", err)
			}

			fmt.Printf("⏰ PAYMENT %s SCADUTO: %d Philia sbloccati per %s\n",
				ev.ID[:8], ev.Amount, ev.Sender[:8])
		}
	}
}

// --- REPUTAZIONE ---

func (e *Engine) GetReputation(userID string) ReputationScore {
	var rep ReputationScore
	rep.UserID = userID

	for _, ev := range e.eventLog {
		switch ev.Type {
		case SETTLE:
			if ev.Recipient == userID && ev.Status == "SETTLED" {
				rep.CompletedSales++
			}
			if ev.Sender == userID && ev.Status == "SETTLED" {
				rep.CompletedPurchases++
			}
		case PAYMENT:
			if ev.Sender == userID && ev.Status == "EXPIRED" {
				rep.ExpiredPayments++
			}
		case ESCROW_DISPUTE:
		}
	}

	        rep.Score = (rep.CompletedSales * 10) +
                (rep.CompletedPurchases * 2) -
                (rep.ExpiredPayments * 5) -
                (rep.DisputesLost * 20)

        rep.Score += e.reputation[userID]

        // 🌱 LOGICA DI BOOTSTRAP: +50 per le identità Genesis
        if e.genesisIDs[userID] {
                rep.Score += 50
        }

        switch {
	case rep.Score >= 100:
		rep.TrustLevel = "VERIFIED"
	case rep.Score >= 50:
		rep.TrustLevel = "TRUSTED"
	case rep.Score >= 10:
		rep.TrustLevel = "ESTABLISHED"
	case rep.Score > 0:
		rep.TrustLevel = "NEW"
	default:
		rep.TrustLevel = "UNTRUSTED"
	}

	return rep
}

func (e *Engine) MaxTransactionAmount(userID string) int64 {
	rep := e.GetReputation(userID)
	switch rep.TrustLevel {
	case "VERIFIED":
		return 100000
	case "TRUSTED":
		return 10000
	case "ESTABLISHED":
		return 1000
	case "NEW":
		return 100
	case "UNTRUSTED":
		return 0
	default:
		return 100
	}
}

// --- ESCROW ---

func (e *Engine) GetEscrow(escrowID string) (*Escrow, error) {
	escrow, exists := e.escrows[escrowID]
	if !exists {
		return nil, errors.New("escrow non trovato")
	}
	return escrow, nil
}

// --- GETTERS ---

func (e *Engine) parentExists(hash string) bool {
	if hash == "" {
		return true
	}
	for _, ev := range e.eventLog {
		if ev.ID == hash {
			return true
		}
	}
	return false
}

func (e *Engine) HasEvent(id string) bool {
	for _, ev := range e.eventLog {
		if ev.ID == id {
			return true
		}
	}
	return false
}

func (e *Engine) GetEventLog() []Event {
	return e.eventLog
}

func (e *Engine) GetBalance(id string) (int64, int64) {
	return e.balances[id], e.reserved[id]
}

func (e *Engine) GetLastHash() string {
	if len(e.eventLog) == 0 {
		return ""
	}
	return e.eventLog[len(e.eventLog)-1].ID
}

func (e *Engine) Close() {
	e.store.SaveSnapshot(e.balances, e.reserved)
}

func (e *Engine) GetRooms() map[string]*Room {
	return e.rooms
}

func (e *Engine) GetMessages(userID string) []Event {
	return e.messages[userID]
}

// GetMessageACKs restituisce tutti gli eventi ACK per un dato messaggio originale
func (e *Engine) GetMessageACKs(originalMsgID string) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ackReceipts[originalMsgID]
}

func (e *Engine) GetAgreements(userID string) []Event {
	return e.agreements[userID]
}

func (e *Engine) GetTransactions(userID string) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var txs []Event
	for _, ev := range e.eventLog {
		if ev.Sender == userID || ev.Recipient == userID {
			txs = append(txs, ev)
		}
	}
	for i, j := 0, len(txs)-1; i < j; i, j = i+1, j-1 {
		txs[i], txs[j] = txs[j], txs[i]
	}
	return txs
}

func (e *Engine) GetRoomsWithReputation() []map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []map[string]interface{}
	for id, room := range e.rooms {
		rep := e.calculateReputationInternal(room.OwnerID)
		result = append(result, map[string]interface{}{
			"id":         id,
			"room":       room,
			"reputation": rep,
		})
	}
	return result
}

func (e *Engine) calculateReputationInternal(userID string) ReputationScore {
	var rep ReputationScore
	rep.UserID = userID

	for _, ev := range e.eventLog {
		switch ev.Type {
		case SETTLE:
			if ev.Recipient == userID && ev.Status == "SETTLED" {
				rep.CompletedSales++
			}
			if ev.Sender == userID && ev.Status == "SETTLED" {
				rep.CompletedPurchases++
			}
		case PAYMENT:
			if ev.Sender == userID && ev.Status == "EXPIRED" {
				rep.ExpiredPayments++
			}
		}
	}

		rep.Score = (rep.CompletedSales * 10) +
		(rep.CompletedPurchases * 2) -
		(rep.ExpiredPayments * 5) -
		(rep.DisputesLost * 20) +
		e.reputation[userID]

	// 🌱 LOGICA DI BOOTSTRAP: +50 per le identità Genesis
	if e.genesisIDs[userID] {
		rep.Score += 50
	}

	switch {
	case rep.Score >= 100:
		rep.TrustLevel = "VERIFIED"
	case rep.Score >= 50:
		rep.TrustLevel = "TRUSTED"
	case rep.Score >= 10:
		rep.TrustLevel = "ESTABLISHED"
	case rep.Score > 0:
		rep.TrustLevel = "NEW"
	default:
		rep.TrustLevel = "UNTRUSTED"
	}

	return rep
}
// findInviterInternal cerca l'invitante di un'identità nel DAG
// Deve essere chiamata SOLO quando il lock è già acquisito
func (e *Engine) findInviterInternal(userID string) string {
	for _, ev := range e.eventLog {
		if ev.Type == "KEY_ANNOUNCE" && ev.Sender == userID {
			return ev.Recipient // L'invitante è nel campo Recipient
		}
	}
	return ""
}

func (e *Engine) SyncHeader() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	lastHash := ""
	if len(e.eventLog) > 0 {
		lastHash = e.eventLog[len(e.eventLog)-1].ID
	}

	hasher := sha256.New()
	for _, ev := range e.eventLog {
		hasher.Write([]byte(ev.ID))
	}
	merkleRoot := fmt.Sprintf("%x", hasher.Sum(nil))

	return map[string]interface{}{
		"total_events": len(e.eventLog),
		"last_hash":    lastHash,
		"merkle_root":  merkleRoot,
	}
}

func (e *Engine) SyncEvents(fromID string, limit int) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	startIndex := 0
	if fromID != "" {
		for i, ev := range e.eventLog {
			if ev.ID == fromID {
				startIndex = i + 1
				break
			}
		}
	}

	endIndex := startIndex + limit
	if endIndex > len(e.eventLog) {
		endIndex = len(e.eventLog)
	}

	if startIndex >= len(e.eventLog) {
		return []Event{}
	}

	return e.eventLog[startIndex:endIndex]
}

func (e *Engine) SyncRecentEvents(userID, contactID string, limit int) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > 500 {
		limit = 50
	}

	var chatEvents []Event

	for _, ev := range e.eventLog {
		if ev.Type != MESSAGE {
			continue
		}
		if ev.Sender == userID && ev.Recipient == contactID {
			chatEvents = append(chatEvents, ev)
			continue
		}
		if ev.Sender == contactID && ev.Recipient == userID {
			chatEvents = append(chatEvents, ev)
			continue
		}
	}

	if len(chatEvents) > limit {
		chatEvents = chatEvents[len(chatEvents)-limit:]
	}

	return chatEvents
}

func (e *Engine) GetChatContacts(userID string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	contactSet := make(map[string]bool)
	for _, ev := range e.eventLog {
		if ev.Type != MESSAGE {
			continue
		}
		if ev.Sender == userID && ev.Recipient != "" {
			contactSet[ev.Recipient] = true
		}
		if ev.Recipient == userID && ev.Sender != "" {
			contactSet[ev.Sender] = true
		}
	}

	var contacts []string
	for c := range contactSet {
		contacts = append(contacts, c)
	}
	return contacts
}

func (e *Engine) GetX25519PubKey(userID string) []byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.x25519Registry[userID]
}

func (e *Engine) processBatch(events []Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var eventsToSave []Event

	for _, ev := range events {
		if e.HasEvent(ev.ID) {
			continue
		}

		if err := ev.ValidateBasic(); err != nil {
			continue
		}
		if err := ev.Verify(); err != nil {
			continue
		}

		if err := e.validateNonce(ev); err != nil {
			continue
		}

				if ev.Type != GENESIS && ev.Type != "KEY_ANNOUNCE" {
			missingParent := false
			if ev.ParentHash != "" && !e.parentExists(ev.ParentHash) {
				missingParent = true
			}
			if !missingParent {
				for _, ref := range ev.ParentRefs {
					if ref != "" && !e.parentExists(ref) {
						missingParent = true
						break
					}
				}
			}
			if missingParent {
				e.orphanPool[ev.ID] = ev
				continue
			}
		}

		if ev.Type == "KEY_ANNOUNCE" {
			keyBytes, err := hex.DecodeString(ev.Memo)
			if err == nil {
				e.x25519Registry[ev.Sender] = keyBytes
			}
		}

		if err := e.applyEventInternal(ev); err != nil {
			continue
		}

		eventsToSave = append(eventsToSave, ev)
		e.eventLog = append(e.eventLog, ev)
	}

	if len(eventsToSave) > 0 {
		if err := e.store.AppendEvents(eventsToSave); err != nil {
			fmt.Printf("⚠️ Errore salvataggio batch: %v\n", err)
		} else {
			fmt.Printf("✅ Salvati %d eventi in batch su BadgerDB\n", len(eventsToSave))
		}
	}

	e.processOrphans()
}

func (e *Engine) GetStatus(userID string) (totalEvents, orphans int, balance, reserved int64, repScore int, trustLevel string) {
	e.mu.RLock()
	totalEvents = len(e.eventLog)
	orphans = len(e.orphanPool)
	if userID != "" {
		balance = e.balances[userID]
		reserved = e.reserved[userID]
	}
	e.mu.RUnlock()

	if userID != "" {
		r := e.GetReputation(userID)
		repScore = r.Score
		trustLevel = r.TrustLevel
	}
	return
}
// CountEventsByTypeInLast24h conta gli eventi di un dato tipo creati da un'identità nelle ultime 24 ore
func (e *Engine) CountEventsByTypeInLast24h(senderID string, eventType string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	now := time.Now().UnixNano()
	cutoff := now - (24 * 60 * 60 * 1e9) // 24 ore fa in nanosecondi
	count := 0
	
	for _, ev := range e.eventLog { // <-- CORRETTO: era e.events
		if ev.Sender == senderID && ev.Type == eventType && ev.Timestamp >= cutoff {
			count++
		}
	}
	
	return count
}
// GetInviteStats calcola quanti inviti ha fatto un'identità e quanto costa il prossimo
func (e *Engine) GetInviteStats(inviterID string) (currentInvites int, maxAllowed int, nextCost int64, currentScore int) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	// 1. Conta tutti gli eventi INVITE fatti da questa identità
	count := 0
	for _, ev := range e.eventLog {
		if ev.Sender == inviterID && ev.Type == INVITE {
			count++
		}
	}
	
	// 2. Ottieni il punteggio di reputazione attuale
	rep := e.GetReputation(inviterID)
	currentScore = rep.Score
	
	// 3. Calcola il massimo consentito: max(5, ⌈Score/20⌉)
	maxAllowed = 5
	if currentScore > 0 {
		ceilScore := (currentScore + 19) / 20 // Trucco per arrotondamento per eccesso in interi
		if ceilScore > 5 {
			maxAllowed = ceilScore
		}
	}
	
	// 4. Calcola il costo del prossimo invito
	if count < 5 {
		nextCost = 1
	} else if count < 10 {
		nextCost = 5
	} else {
		nextCost = 25
	}
	
	return count, maxAllowed, nextCost, currentScore
}

// GetAdaptiveParentRefs restituisce gli ID degli eventi recenti da usare come genitori nel DAG.
// Implementa la logica adattiva: max(60s, 3 * T_lat). Qui usiamo una finestra di 60 secondi, max 3 riferimenti.
func (e *Engine) GetAdaptiveParentRefs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var refs []string
	now := time.Now().UnixNano()
	window := int64(60 * 1e9) // 60 secondi in nanosecondi
	cutoff := now - window

	count := 0
	// Scansiona all'indietro dall'evento più recente
	for i := len(e.eventLog) - 1; i >= 0 && count < 3; i-- {
		ev := e.eventLog[i]
		if ev.Timestamp >= cutoff {
			refs = append(refs, ev.ID)
			count++
		}
	}

	// Fallback di sicurezza: se il DAG è vuoto o nessun evento rientra nella finestra, 
	// usa almeno l'ultimo evento assoluto per non spezzare la catena.
	if len(refs) == 0 && len(e.eventLog) > 0 {
		refs = append(refs, e.eventLog[len(e.eventLog)-1].ID)
	}

	return refs
}