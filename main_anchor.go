//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	fmt.Println("⚓ --- PEP DAILY ANCHORING TOOL --- ⚓")
	fmt.Println("Generazione della prova crittografica di immutabilità...\n")

	// Per questo test, usiamo l'ultima cartella data_dag_A creata, o fallback su "data"
	// In produzione, questo punterebbe a "data/events.log"
	logDir := "data" // Cambia in "data_dag_A_..." se vuoi testare su quello specifico
	
	logFilePath := filepath.Join(logDir, "events.log")
	anchorFilePath := filepath.Join(logDir, "anchors.json")
	
	// Verifica che il file di log esista
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		fmt.Printf("⚠️ Nessun file di log trovato in %s. Esegui prima una transazione.\n", logFilePath)
		fmt.Println("💡 Suggerimento: esegui 'go run main.go' o 'go run main_dag.go ...' prima di ancorare.")
		return
	}

	// Data di oggi (o data simulata)
	today := time.Now().Format("2006-01-02")

	// Esegui l'ancoraggio
	err := ExecuteDailyAnchor(logFilePath, anchorFilePath, today)
	if err != nil {
		fmt.Printf("\n🚨 ERRORE CRITICO: %v\n", err)
		return
	}

	fmt.Println("\n🏆 ANCORAGGIO COMPLETATO CON SUCCESSO!")
	fmt.Println("Il registro di oggi è ora crittograficamente vincolato a una prova esterna.")
	fmt.Println("Qualsiasi modifica futura al file events.log invalidarà questa prova.")
}