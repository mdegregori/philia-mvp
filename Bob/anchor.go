package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

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
func SimulateBlockchainAnchor(logHash string) (string, error) {
	fmt.Println("📡 Connessione alla rete esterna (simulata)...")
	time.Sleep(1 * time.Second)

	mockData := logHash + fmt.Sprintf("%d", time.Now().UnixNano())
	hash := sha256.Sum256([]byte(mockData))

	return "0x" + hex.EncodeToString(hash[:])[:40], nil
}

// SaveAnchorRecord salva la prova di ancoraggio in un file locale (es. data/anchors.json)
func SaveAnchorRecord(record AnchorRecord, anchorFilePath string) error {
	var records []AnchorRecord
	if data, err := os.ReadFile(anchorFilePath); err == nil {
		_ = json.Unmarshal(data, &records)
	}

	records = append(records, record)

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

// ExecuteDailyAnchor accetta 3 argomenti: (nodeID, logPath, anchorPath)
func ExecuteDailyAnchor(nodeID string, logPath string, anchorPath string) error {
	logHash, err := ComputeLogHash(logPath)
	if err != nil {
		return fmt.Errorf("errore calcolo log hash per %s: %w", nodeID, err)
	}

	txHash, err := SimulateBlockchainAnchor(logHash)
	if err != nil {
		return fmt.Errorf("errore simulazione ancoraggio: %w", err)
	}

	record := AnchorRecord{
		Timestamp: time.Now().Unix(),
		LogHash:   logHash,
		TxHash:    txHash,
		Status:    "CONFIRMED",
	}

	if err := SaveAnchorRecord(record, anchorPath); err != nil {
		return fmt.Errorf("errore salvataggio record: %w", err)
	}

	fmt.Printf("⚓ Ancoraggio completato per nodo %s! TxHash: %s\n", nodeID, txHash)
	return nil
}