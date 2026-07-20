package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// --- COSTANTI PER I TIPI DI EVENTO ---
const (
	GENESIS        = "GENESIS"
	PAYMENT        = "PAYMENT"
	SETTLE         = "SETTLE"
	MESSAGE        = "MESSAGE"
	AGREEMENT      = "AGREEMENT"
	ROOM_CREATE    = "ROOM_CREATE"
	ROOM_UPDATE    = "ROOM_UPDATE"
	ESCROW_LOCK    = "ESCROW_LOCK"
	ESCROW_RELEASE = "ESCROW_RELEASE"
	ESCROW_DISPUTE = "ESCROW_DISPUTE"
)

// --- STRUTTURE DATI PER LA SUPERAPP ---

// Room rappresenta un'offerta (B&B, prodotto, servizio)
type Room struct {
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	IsPublic    bool   `json:"is_public"`
	BasePrice   int64  `json:"base_price"`
	CreatedAt   int64  `json:"created_at"`
}

// Message rappresenta un messaggio crittografato tra due peer
type Message struct {
	ID          string `json:"id"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
	Content     string `json:"content"`
	Timestamp   int64  `json:"timestamp"`
}

// Escrow rappresenta un contratto multi-firma (2-of-3) per transazioni tra sconosciuti
type Escrow struct {
	ID           string            `json:"id"`
	Buyer        string            `json:"buyer"`
	Seller       string            `json:"seller"`
	Arbitrator   string            `json:"arbitrator"`
	Amount       int64             `json:"amount"`
	Description  string            `json:"description"`
	RequiredSigs int               `json:"required_sigs"`
	Signatures   map[string]string `json:"signatures"` // pubkey -> signature hex
	Status       string            `json:"status"`     // LOCKED, RELEASED, DISPUTED, REFUNDED
	CreatedAt    int64             `json:"created_at"`
	ExpiresAt    int64             `json:"expires_at"`
}

// ReputationScore rappresenta il punteggio di fiducia calcolato on-DAG
type ReputationScore struct {
	UserID             string `json:"user_id"`
	CompletedSales     int    `json:"completed_sales"`
	CompletedPurchases int    `json:"completed_purchases"`
	ExpiredPayments    int    `json:"expired_payments"`
	DisputesLost       int    `json:"disputes_lost"`
	Score              int    `json:"score"`
	TrustLevel         string `json:"trust_level"` // UNTRUSTED, NEW, ESTABLISHED, TRUSTED, VERIFIED
}

// --- STRUTTURA EVENTO DAG ---

// Event è l'unità fondamentale del registro PEP
type Event struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	ParentHash string `json:"parent_hash"`
	Sender     string `json:"sender"`
	Recipient  string `json:"recipient"`
	Amount     int64  `json:"amount"`
	Timestamp  int64  `json:"timestamp"`
	Nonce      uint64 `json:"nonce"`       // Anti-replay: contatore monotono
	TTLSeconds int64  `json:"ttl_seconds"` // Durata in secondi (0 = nessun TTL)
	ExpiresAt  int64  `json:"expires_at"`  // Timestamp di scadenza
	Memo       string `json:"memo"`
	Status     string `json:"status"`      // PENDING, SETTLED, EXPIRED, INVALID

	PubKey    string `json:"pub_key"`
	Signature string `json:"signature"`
}

// --- METODI DELL'EVENTO ---

// NewEvent crea un nuovo evento vuoto
func NewEvent(eventType, parentHash, sender, recipient string, amount int64, timestamp int64, nonce uint64, ttlSeconds int64, memo string) Event {
	ev := Event{
		Type:       eventType,
		ParentHash: parentHash,
		Sender:     sender,
		Recipient:  recipient,
		Amount:     amount,
		Timestamp:  timestamp,
		Nonce:      nonce,
		TTLSeconds: ttlSeconds,
		Memo:       memo,
		Status:     "PENDING",
	}

	// Calcola la scadenza se il TTL è specificato
	if ttlSeconds > 0 {
		ev.ExpiresAt = timestamp + (ttlSeconds * 1000000000) // Converti secondi in nanosecondi
	}

	return ev
}

// CalculateID calcola l'ID univoco dell'evento basato sui suoi campi
// NOTA: Include Nonce e TTLSeconds per prevenire manipolazioni
func (e *Event) CalculateID() {
	data := fmt.Sprintf("%s|%s|%s|%s|%d|%d|%d|%d|%s",
		e.Type, e.ParentHash, e.Sender, e.Recipient,
		e.Amount, e.Timestamp, e.Nonce, e.TTLSeconds, e.Memo)
	hash := sha256.Sum256([]byte(data))
	e.ID = hex.EncodeToString(hash[:])
}

// Sign firma l'evento con la chiave privata del mittente
func (e *Event) Sign(privKey ed25519.PrivateKey) {
	e.CalculateID()
	e.PubKey = hex.EncodeToString(privKey.Public().(ed25519.PublicKey))
	signature := ed25519.Sign(privKey, []byte(e.ID))
	e.Signature = hex.EncodeToString(signature)
}

// Verify verifica la firma dell'evento e l'integrità dei dati
func (e *Event) Verify() error {
	if e.Signature == "" || e.PubKey == "" {
		return errors.New("missing signature or public key")
	}

	pubKeyBytes, err := hex.DecodeString(e.PubKey)
	if err != nil {
		return err
	}

	sigBytes, err := hex.DecodeString(e.Signature)
	if err != nil {
		return err
	}

	// Ricalcoliamo l'ID per assicurarci che non sia stato manipolato
	originalID := e.ID
	e.CalculateID()
	if e.ID != originalID {
		return errors.New("event data has been tampered with")
	}

	if !ed25519.Verify(pubKeyBytes, []byte(e.ID), sigBytes) {
		return errors.New("invalid signature")
	}

	return nil
}

// ValidateBasic controlla la validità strutturale dell'evento
func (e *Event) ValidateBasic() error {
	if e.Type == "" {
		return errors.New("event type cannot be empty")
	}
	if e.Sender == "" {
		return errors.New("sender cannot be empty")
	}
	if e.Timestamp == 0 {
		return errors.New("timestamp cannot be zero")
	}
	return nil
}

// IsExpired controlla se l'evento è scaduto (per PAYMENT con TTL)
func (e *Event) IsExpired() bool {
	if e.ExpiresAt == 0 {
		return false
	}
	return time.Now().UnixNano() > e.ExpiresAt
}

// ToJSON converte l'evento in JSON (utile per la rete libp2p)
func (e *Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// --- FUNZIONI DI SUPPORTO ---

func PubKeyToID(pubKey ed25519.PublicKey) string {
	return hex.EncodeToString(pubKey)
}

// GetTimeNow restituisce il timestamp corrente in nanosecondi
func GetTimeNow() int64 {
	return time.Now().UnixNano()
}