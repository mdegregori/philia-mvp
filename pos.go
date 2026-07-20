package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AnchorRecord rappresenta un record di notarizzazione su blockchain
type AnchorRecord struct {
	Date        string `json:"date"`
	EventCount  int    `json:"event_count"`
	LogHash     string `json:"log_hash"`
	TxHash      string `json:"tx_hash"`
	BlockNumber int64  `json:"block_number"`
}

// ExecuteDailyAnchor crea un anchor giornaliero del registro
func ExecuteDailyAnchor(logPath, anchorPath, date string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return fmt.Errorf("errore lettura log: %w", err)
	}

	// Calcola hash del log corrente
	currentHash := fmt.Sprintf("%x", sha256.Sum256(data))

	// Carica record esistenti
	var records []AnchorRecord
	if existing, err := os.ReadFile(anchorPath); err == nil {
		json.Unmarshal(existing, &records)
	}

	// Crea nuovo record
	record := AnchorRecord{
		Date:        date,
		EventCount:  countEvents(data),
		LogHash:     currentHash,
		TxHash:      simulateBlockchainTx(currentHash),
		BlockNumber: time.Now().Unix(),
	}

	records = append(records, record)

	// Salva
	data, _ = json.MarshalIndent(records, "", "  ")
	if err := os.WriteFile(anchorPath, data, 0644); err != nil {
		return fmt.Errorf("errore salvataggio anchor: %w", err)
	}

	fmt.Printf("✅ Anchor creato per %s: %s\n", date, record.TxHash)
	return nil
}

func countEvents(data []byte) int {
	// Conta le linee nel log (semplificato)
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

func simulateBlockchainTx(hash string) string {
	// Simula una transazione blockchain (in produzione sarebbe reale)
	return fmt.Sprintf("0x%s", hash[:40])
}

// CreateAnchorEvent crea un evento di anchor nel DAG
func CreateAnchorEvent(parentHash, sender string, anchorData string, nonce uint64) Event {
	return NewEvent(
		"ANCHOR",
		parentHash,
		sender,
		"",
		0,
		time.Now().UnixNano(),
		nonce,
		0, // No TTL per anchor
		anchorData,
	)
}

// VerifyAnchor verifica un anchor contro il log corrente
func VerifyAnchor(anchorPath, logPath string) error {
	anchorData, err := os.ReadFile(anchorPath)
	if err != nil {
		return fmt.Errorf("errore lettura anchor: %w", err)
	}

	var records []AnchorRecord
	if err := json.Unmarshal(anchorData, &records); err != nil {
		return fmt.Errorf("errore parsing anchor: %w", err)
	}

	if len(records) == 0 {
		return fmt.Errorf("nessun record anchor")
	}

	lastRecord := records[len(records)-1]
	currentData, _ := os.ReadFile(logPath)
	currentHash := fmt.Sprintf("%x", sha256.Sum256(currentData))

	if currentHash != lastRecord.LogHash {
		return fmt.Errorf("hash mismatch: current %s != anchored %s", currentHash, lastRecord.LogHash)
	}

	fmt.Println("✅ Anchor verificato con successo")
	return nil
}

// GetAnchorRecord recupera un record anchor per data
func GetAnchorRecord(anchorPath, date string) (*AnchorRecord, error) {
	data, err := os.ReadFile(anchorPath)
	if err != nil {
		return nil, fmt.Errorf("errore lettura anchor: %w", err)
	}

	var records []AnchorRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("errore parsing anchor: %w", err)
	}

	for _, record := range records {
		if record.Date == date {
			return &record, nil
		}
	}

	return nil, fmt.Errorf("anchor non trovato per %s", date)
}