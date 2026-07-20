package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
        "crypto/sha256"
)

// --- STRUTTURA ENGINE ---

type Engine struct {
	store      *Store
	balances   map[string]int64       // SALDO CONTABILE (Ledger)
	reserved   map[string]int64       // PARTITE PRENOTATE (Reserved)
	nonces     map[string]uint64      // Anti-replay: ultimo nonce visto per identità
	reputation map[string]int         // Punteggio reputazione (calcolato on-DAG)
	rooms      map[string]*Room
	escrows    map[string]*Escrow     // Contratti multi-firma attivi
	eventLog   []Event
	orphanPool map[string]Event
	messages   map[string][]Event
	agreements map[string][]Event
	mu         sync.RWMutex
}

// --- COSTRUZIONE ---

func NewEngine(store *Store) *Engine {
	engine := &Engine{
		store:      store,
		balances:   make(map[string]int64),
		reserved:   make(map[string]int64),
		nonces:     make(map[string]uint64),
		reputation: make(map[string]int),
		rooms:      make(map[string]*Room),
		escrows:    make(map[string]*Escrow),
		orphanPool: make(map[string]Event),
		messages:   make(map[string][]Event),
		agreements: make(map[string][]Event),
	}

	events, err := store.LoadEvents()
	if err != nil {
		fmt.Println("Nessun evento precedente trovato. Avvio da zero.")
		return engine
	}

	fmt.Printf("✅ Engine loaded. Replay di %d eventi dal disco.\n", len(events))
	for _, ev := range events {
		engine.applyEventInternal(ev)
		engine.eventLog = append(engine.eventLog, ev)
		// Ricostruisci i nonce dal log
		if ev.Nonce > engine.nonces[ev.Sender] {
			engine.nonces[ev.Sender] = ev.Nonce
		}
	}
	return engine
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
	if ev.Type != GENESIS {
		if !e.parentExists(ev.ParentHash) {
			fmt.Printf("⏳ Evento %s è ORFANO (attesa genitore %s)\n", ev.ID[:8], ev.ParentHash[:8])
			e.orphanPool[ev.ID] = ev
			return nil
		}
	}

	// 6. Verifica double spend (se PAYMENT)
	if ev.Type == PAYMENT {
		e.checkDoubleSpend(ev)
	}

	// 7. Applica evento
	if err := e.applyEventInternal(ev); err != nil {
		return err
	}

	// 8. Persisti (append-only)
	if err := e.store.AppendEvent(ev); err != nil {
		return fmt.Errorf("errore salvataggio: %w", err)
	}

	// 9. Aggiorna log e processa orfani
	e.eventLog = append(e.eventLog, ev)
	e.processOrphans()

	return nil
}

// --- ORPHAN POOL ---

func (e *Engine) processOrphans() {
	for {
		processed := false
		for id, ev := range e.orphanPool {
			if e.parentExists(ev.ParentHash) {
				// Verifica nonce prima di applicare
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
				processed = true
			}
		}
		if !processed {
			break
		}
	}
	if len(e.orphanPool) > 0 {
		fmt.Printf("⚠️ %d eventi ancora orfani (genitori mancanti)\n", len(e.orphanPool))
	}
}

// --- APPLICAZIONE EVENTI (CORE) ---

func (e *Engine) applyEventInternal(ev Event) error {
	switch ev.Type {

	case GENESIS:
		e.balances[ev.Sender] += ev.Amount
		fmt.Printf("GENESIS: Account %s balance updated to %d\n", ev.Sender[:8], e.balances[ev.Sender])

	case PAYMENT:
		// Controlla se il PAYMENT è scaduto (TTL)
		if ev.IsExpired() {
			fmt.Printf(" PAYMENT %s SCADUTO (TTL superato), fondi sbloccati\n", ev.ID[:8])
			ev.Status = "EXPIRED"
			return nil
		}
		// Verifica saldo disponibile (Ledger - Reserved)
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
		// Incrementa reputazione del destinatario (venditore)
		e.reputation[ev.Recipient] += 10
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
		fmt.Printf(" ROOM aggiornata: '%s' (ID: %s...)\n", room.Name, room.ID[:8])

	case MESSAGE:
		e.messages[ev.Recipient] = append(e.messages[ev.Recipient], ev)
		fmt.Printf("💬 Messaggio da %s a %s: %s\n", ev.Sender[:8], ev.Recipient[:8], ev.Memo)

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
		// Registra la firma
		escrow.Signatures[ev.Sender] = ev.Signature
		fmt.Printf("✍️ Firma ESCROW %s da %s (%d/%d)\n", 
			escrow.ID[:8], ev.Sender[:8], len(escrow.Signatures), escrow.RequiredSigs)
		
		// Se raggiunte le firme richieste, rilascia i fondi
		if len(escrow.Signatures) >= escrow.RequiredSigs {
			e.reserved[escrow.Buyer] -= escrow.Amount
			e.balances[escrow.Buyer] -= escrow.Amount
			e.balances[escrow.Seller] += escrow.Amount
			escrow.Status = "RELEASED"
			e.reputation[escrow.Seller] += 10
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
		fmt.Printf("️ DISPUTA aperta su ESCROW %s: %s\n", escrow.ID[:8], dispute.Description)

	default:
		return fmt.Errorf("tipo evento sconosciuto: %s", ev.Type)
	}

	return nil
}

// --- CONSENSO CLO (CAUSAL LEXICOGRAPHIC ORDERING) ---

// resolveConflict applica l'algoritmo CLO per risolvere double spend offline.
// Non usa timestamp (inaffidabile), ma Nonce e ID (deterministici).
func (e *Engine) resolveConflict(a, b Event) Event {
	// Fase 1: Confronta Nonce (monotono, non manipolabile retroattivamente)
	if a.Nonce != b.Nonce {
		if a.Nonce < b.Nonce {
			return a
		}
		return b
	}
	// Fase 2: Tie-breaking lexicografico sull'ID (hash deterministico)
	if a.ID < b.ID {
		return a
	}
	return b
}

// checkDoubleSpend verifica se un PAYMENT compete con altri PAYMENT per gli stessi fondi.
// Se trova un conflitto, risolve con CLO e invalida il perdente.
func (e *Engine) checkDoubleSpend(ev Event) {
	// Cerca altri PAYMENT pendenti dallo stesso sender che potrebbero competere
	available := e.balances[ev.Sender] - e.reserved[ev.Sender]
	
	// Se c'è abbastanza saldo disponibile, nessun conflitto
	if available >= ev.Amount {
		return
	}
	
	// Altrimenti, cerca nel log eventi PAYMENT concorrenti
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
		// Se i due eventi sono concorrenti (nessuno è ancestor dell'altro)
		if oldEv.ParentHash != ev.ParentHash && oldEv.ID != ev.ParentHash && ev.ID != oldEv.ParentHash {
			// Conflitto rilevato: applica CLO
			winner := e.resolveConflict(ev, oldEv)
			if winner.ID == ev.ID {
				// Il nuovo evento vince, invalida il vecchio
				oldEv.Status = "INVALID"
				e.reserved[oldEv.Sender] -= oldEv.Amount
				fmt.Printf("⚔️ CLO: vince nuovo evento %s (nonce %d), invalidato %s\n", 
					ev.ID[:8], ev.Nonce, oldEv.ID[:8])
			} else {
				// Il vecchio evento vince, rigetta il nuovo
				ev.Status = "INVALID"
				fmt.Printf("⚔️ CLO: vince evento esistente %s (nonce %d), rigettato %s\n", 
					oldEv.ID[:8], oldEv.Nonce, ev.ID[:8])
				return // Non applicare il nuovo evento
			}
		}
	}
}

// --- ANTI-REPLAY ---

func (e *Engine) validateNonce(ev Event) error {
	lastSeen := e.nonces[ev.Sender]
	// Il nonce deve essere strettamente maggiore dell'ultimo visto
	if ev.Nonce <= lastSeen {
		return fmt.Errorf("REPLAY ATTACK: nonce %d già usato (ultimo visto: %d)", ev.Nonce, lastSeen)
	}
	// Aggiorna il nonce solo se è il successivo atteso (previene salti)
	if ev.Nonce == lastSeen+1 {
		e.nonces[ev.Sender] = ev.Nonce
	}
	return nil
}

// --- TTL & SCADENZA ---

// checkExpiredPayments scansiona il log e sblocca i fondi dei PAYMENT scaduti.
// Da chiamare periodicamente o prima di operazioni critiche.
func (e *Engine) checkExpiredPayments() {
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
			// PAYMENT scaduto: sblocca i fondi
			e.reserved[ev.Sender] -= ev.Amount
			ev.Status = "EXPIRED"
			// Penalizza reputazione del mittente (pagamento non completato)
			e.reputation[ev.Sender] -= 5
			fmt.Printf("⏰ PAYMENT %s SCADUTO: sbloccati %d Philia a %s\n", 
				ev.ID[:8], ev.Amount, ev.Sender[:8])
		}
	}
}

// --- REPUTAZIONE ---

// GetReputation calcola e restituisce il punteggio di reputazione per un utente.
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
			// Logica semplificata: chi apre la disputa e perde
			// (in produzione servirebbe tracciare l'esito)
		}
	}

	// Formula punteggio (come da documento v1.1 §3.7.2)
	rep.Score = (rep.CompletedSales * 10) +
		(rep.CompletedPurchases * 2) -
		(rep.ExpiredPayments * 5) -
		(rep.DisputesLost * 20)

	// Aggiungi il punteggio base dal contatore reputation (incrementato nei SETTLE)
	rep.Score += e.reputation[userID]

	// Livelli di fiducia
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

// MaxTransactionAmount restituisce il limite di transazione basato sulla reputazione.
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
		return 0 // Deve usare escrow
	default:
		return 100
	}
}

// --- ESCROW ---

// GetEscrow restituisce un contratto escrow per ID.
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

func (e *Engine) GetAgreements(userID string) []Event {
	return e.agreements[userID]
}

// GetTransactions restituisce tutti gli eventi in cui l'utente è sender o recipient
func (e *Engine) GetTransactions(userID string) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	var txs []Event
	for _, ev := range e.eventLog {
		if ev.Sender == userID || ev.Recipient == userID {
			txs = append(txs, ev)
		}
	}
	// Ordina dal più recente al più vecchio
	for i, j := 0, len(txs)-1; i < j; i, j = i+1, j-1 {
		txs[i], txs[j] = txs[j], txs[i]
	}
	return txs
}

// GetRoomsWithReputation restituisce le room con la reputazione del proprietario
func (e *Engine) GetRoomsWithReputation() []map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	var result []map[string]interface{}
	for id, room := range e.rooms {
		rep := e.calculateReputationInternal(room.OwnerID)
		result = append(result, map[string]interface{}{
			"id":           id,
			"room":         room,
			"reputation":   rep,
		})
	}
	return result
}

// calculateReputationInternal (versione interna senza lock)
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

// SyncHeader restituisce informazioni per la sincronizzazione
func (e *Engine) SyncHeader() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	lastHash := ""
	if len(e.eventLog) > 0 {
		lastHash = e.eventLog[len(e.eventLog)-1].ID
	}
	
	// Calcola un merkle root semplificato (hash di tutti gli ID)
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

// SyncEvents restituisce eventi in blocchi, partendo da fromID
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



	