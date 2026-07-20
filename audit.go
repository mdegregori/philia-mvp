//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	fmt.Println("🛡️ --- PEP AUDIT TOOL v1.0 --- 🛡️")
	fmt.Println("Verifica di integrità crittografica e immutabilità del ledger...\n")

	logFile := "data/events.log"
	file, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("⚠️ Nessun file di log trovato. Il ledger è vuoto.")
			return
		}
		fmt.Printf("❌ Errore critico: impossibile aprire %s: %v\n", logFile, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	var totalEvents int
	var validEvents int
	var invalidEvents int

	seenIDs := make(map[string]bool)

	fmt.Println("🔍 Analisi in corso...")
	fmt.Println("------------------------------------------------------------")

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			fmt.Printf("❌ Riga %d: Errore di parsing JSON\n", totalEvents+1)
			invalidEvents++
			totalEvents++
			continue
		}

		totalEvents++
		isValid := true
		var errors []string

		// CHECK 1: Integrità dell'Hash (i dati non sono stati alterati)
		computedHash := ev.ComputeHash()
		if ev.ID != computedHash {
			isValid = false
			errors = append(errors, "Hash non corrispondente")
		}

		// CHECK 2: Validità della Firma Crittografica
		if err := ev.Verify(); err != nil {
			isValid = false
			errors = append(errors, fmt.Sprintf("Firma non valida: %v", err))
		}

		// CHECK 3: Integrità della Catena DAG (il Genitore deve esistere ed essere precedente)
		if ev.Type != "GENESIS" && ev.ParentHash != "" {
			if !seenIDs[ev.ParentHash] {
				isValid = false
				errors = append(errors, "Genitore mancante o fuori ordine")
			}
		}

		if isValid {
			validEvents++
			seenIDs[ev.ID] = true
			fmt.Printf("✅ Evento %d: %-7s | ID: %s... | FIRMA OK | CATENA OK\n",
				totalEvents, ev.Type, ev.ID[:8])
		} else {
			invalidEvents++
			fmt.Printf("❌ Evento %d: %-7s | ID: %s... | ERRORI: %v\n",
				totalEvents, ev.Type, ev.ID[:8], errors)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("\n⚠️ Errore di lettura del file: %v\n", err)
	}

	fmt.Println("------------------------------------------------------------")
	fmt.Printf("📊 RIEPILOGO AUDIT:\n")
	fmt.Printf("   Totale eventi analizzati: %d\n", totalEvents)
	fmt.Printf("   Eventi validi:            %d\n", validEvents)
	fmt.Printf("   Eventi invalidi:          %d\n", invalidEvents)
	fmt.Println("------------------------------------------------------------")

	if invalidEvents == 0 && totalEvents > 0 {
		fmt.Println("🏆 ✅ AUDIT PASSED: Il ledger è crittograficamente integro e immutabile.")
		fmt.Println("   I dati sono pronti per la revisione delle autorità.")
	} else if totalEvents == 0 {
		fmt.Println("ℹ️ Nessun dato da auditare.")
	} else {
		fmt.Println("🚨 ❌ AUDIT FAILED: Sono state rilevate manomissioni o errori crittografici.")
		fmt.Println("   NON utilizzare questi dati per scopi legali o finanziari.")
	}
}