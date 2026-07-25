package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// NOTA: La struct AnchorRecord è stata rimossa.
// Si usa quella definita in types.go che è la fonte di verità.

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
// Usa scrittura atomica (scrive su .tmp poi rinomina) per evitare corruzioni
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

// NOTA: ExecuteDailyAnchor è stata rimossa da qui.
// La funzione rimane solo in pos.go per evitare duplicazioni.
// Le funzioni helper sopra (ComputeLogHash, SimulateBlockchainAnchor, SaveAnchorRecord)
// possono essere chiamate da pos.go o da qualsiasi altro file del package main.