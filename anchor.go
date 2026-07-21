package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// AnchorRecord rappresenta la prova crittografica salvata localmente
type AnchorRecord struct {
	Date        string `json:"date"`         // Data di riferimento (es. "2026-06-15")
	LogHash     string `json:"log_hash"`     // Hash SHA-256 del file events.log
	TxHash      string `json:"tx_hash"`      // Hash della transazione sulla blockchain esterna (simulato)
	Timestamp   int64  `json:"timestamp"`    // Timestamp UNIX dell'ancoraggio
	Blockchain  string `json:"blockchain"`   // Nome della blockchain o servizio notarile
}

// ComputeLogHash legge l'intero file e restituisce il suo hash SHA-256
func ComputeLogHash(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("impossibile leggere il file di log: %w", err)
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// SimulateBlockchainAnchor simula l'invio dell'hash a una blockchain pubblica
// In produzione, qui useresti l'SDK di Stellar, Celo o Ethereum.
func SimulateBlockchainAnchor(logHash string) (string, error) {
	fmt.Println("📡 Connessione alla rete esterna (simulata)...")
	time.Sleep(1 * time.Second) // Simula la latenza di rete

	// Generiamo un "TxHash" fittizio ma realistico basato sull'hash del log + timestamp
	mockData := logHash + fmt.Sprintf("%d", time.Now().UnixNano())
	hash := sha256.Sum256([]byte(mockData))

	return "0x" + hex.EncodeToString(hash[:])[:40], nil // Simula un hash di transazione
}

// SaveAnchorRecord salva la prova di ancoraggio in un file locale (es. data/anchors.json)
func SaveAnchorRecord(record AnchorRecord, anchorFilePath string) error {
	// Leggiamo i record esistenti, se il file esiste
	var records []AnchorRecord
	if data, err := os.ReadFile(anchorFilePath); err == nil {
		json.Unmarshal(data, &records)
	}

	// Aggiungiamo il nuovo record
	records = append(records, record)

	// Salviamo tutto in modo atomico (scrive su .tmp poi rinomina)
	jsonData, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := anchorFilePath + ".tmp"
	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		return err
	}

	return os.Rename(tmpFile, anchorFilePath)
}

// ExecuteDailyAnchor è la funzione principale che orchestra tutto il processo
func ExecuteDailyAnchor(logFilePath string, anchorFilePath string, date string) error {
	fmt.Printf("🔒 Avvio procedura di ancoraggio giornaliero per la data: %s\n", date)

	// 1. Calcola l'hash del registro
	logHash, err := ComputeLogHash(logFilePath)
	if err != nil {
		return fmt.Errorf("fallimento calcolo hash: %w", err)
	}
	fmt.Printf("✅ Hash del registro calcolato: %s...\n", logHash[:16])

	// 2. Simula l'ancoraggio esterno
	txHash, err := SimulateBlockchainAnchor(logHash)
	if err != nil {
		return fmt.Errorf("fallimento ancoraggio esterno: %w", err)
	}
	fmt.Printf("✅ Ancorato sulla blockchain con TxHash: %s...\n", txHash[:16])

	// 3. Salva la ricevuta locale
	record := AnchorRecord{
		Date:       date,
		LogHash:    logHash,
		TxHash:     txHash,
		Timestamp:  time.Now().Unix(),
		Blockchain: "Stellar Testnet (Simulato)",
	}

	if err := SaveAnchorRecord(record, anchorFilePath); err != nil {
		return fmt.Errorf("fallimento salvataggio ricevuta: %w", err)
	}
	fmt.Printf("✅ Ricevuta notarile salvata in: %s\n", anchorFilePath)

	return nil
}