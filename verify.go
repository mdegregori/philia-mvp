//go:build ignore

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AnchorRecord struct {
	Date       string `json:"date"`
	LogHash    string `json:"log_hash"`
	TxHash     string `json:"tx_hash"`
	Timestamp  int64  `json:"timestamp"`
	Blockchain string `json:"blockchain"`
}

func main() {
	fmt.Println("🛡️ --- PEP INTEGRITY VERIFICATION TOOL --- 🛡️")
	
	logFilePath := filepath.Join("data", "events.log")
	anchorFilePath := filepath.Join("data", "anchors.json")

	// 1. Leggi l'ultimo record di ancoraggio (il "sigillo" originale)
	data, err := os.ReadFile(anchorFilePath)
	if err != nil {
		fmt.Println("❌ Impossibile leggere il file di ancoraggio. Esegui prima l'ancoraggio.")
		return
	}
	var records []AnchorRecord
	json.Unmarshal(data, &records)
	
	if len(records) == 0 {
		fmt.Println("❌ Nessun record di ancoraggio trovato.")
		return
	}
	
	// Prendiamo il penultimo record (quello prima della tua eventuale manomissione) 
	// o l'ultimo se ne esiste solo uno.
	targetRecord := records[0]
	if len(records) > 1 {
		targetRecord = records[len(records)-2] // Quello "buono" prima della modifica
	}
	
	fmt.Printf("📜 Sigillo notarile di riferimento (Data: %s)\n", targetRecord.Date)
	fmt.Printf("🔒 Hash salvato nel sigillo: %s\n", targetRecord.LogHash)

	// 2. Calcola l'hash del file events.log attuale
	currentData, err := os.ReadFile(logFilePath)
	if err != nil {
		fmt.Printf("❌ Impossibile leggere il file di log: %v\n", err)
		return
	}
	currentHashBytes := sha256.Sum256(currentData)
	currentHash := hex.EncodeToString(currentHashBytes[:])
	
	fmt.Printf("🔍 Hash calcolato dal file attuale: %s\n", currentHash)

	// 3. Confronto bit-a-bit
	fmt.Println("\n--- VERDETTO ---")
	if currentHash == targetRecord.LogHash {
		fmt.Println("✅ INTEGRITÀ CONFERMATA: Il file non è stato alterato dall'ultimo ancoraggio valido.")
	} else {
		fmt.Println("🚨 ALLARME MANOMISSIONE: L'hash del file attuale NON corrisponde al sigillo notarile!")
		fmt.Println("⚠️ Il file events.log è stato modificato illegalmente dopo l'ancoraggio.")
	}
}