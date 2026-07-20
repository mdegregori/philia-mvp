//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("🧹 --- PEP FRESH START (RESET TOTALE) --- 🧹")
	
	os.RemoveAll("data")
	os.MkdirAll("data", 0755)
	
	store := NewStore("data")
	engine := NewEngine(store)
	defer engine.Close()

	pubH, privH, _ := ed25519.GenerateKey(rand.Reader)
	idH := hex.EncodeToString(pubH)
	
	pubG, privG, _ := ed25519.GenerateKey(rand.Reader)
	idG := hex.EncodeToString(pubG)

	fmt.Println("\n1️⃣ Creazione GENESIS Hotel...")
	genH := NewEvent(GENESIS, "", idH, "", 5000, time.Now().UnixNano(), "Hotel Fresh")
	genH.Sign(privH)
	if err := engine.ProcessEvent(genH); err != nil { fmt.Println("❌ ERRORE:", err); return }

	fmt.Println("2️⃣ Creazione GENESIS Ospite...")
	genG := NewEvent(GENESIS, "", idG, "", 1000, time.Now().UnixNano(), "Guest Fresh")
	genG.Sign(privG)
	if err := engine.ProcessEvent(genG); err != nil { fmt.Println("❌ ERRORE:", err); return }

	fmt.Println("3️⃣ Creazione PAYMENT (Check-in)...")
	pay := NewEvent(PAYMENT, genG.ID, idG, idH, 250, time.Now().UnixNano(), "ROOM_101")
	pay.Sign(privG)
	if err := engine.ProcessEvent(pay); err != nil { fmt.Println("❌ ERRORE:", err); return }

	fmt.Println("4️⃣ Creazione SETTLE (Check-out)...")
	settle := NewEvent(SETTLE, pay.ID, idG, idH, 250, time.Now().UnixNano(), "ROOM_101")
	settle.Sign(privG)
	if err := engine.ProcessEvent(settle); err != nil { fmt.Println("❌ ERRORE:", err); return }

	fmt.Println("\n💾 Salvataggio snapshot finale su disco...")
	engine.Close()
	
	if _, err := os.Stat("data/events.log"); err == nil {
		fmt.Println("✅ SUCCESSO ASSOLUTO: data/events.log è stato creato con una catena DAG valida!")
	} else {
		fmt.Println("❌ ERRORE CRITICO: events.log non è stato creato.")
	}
}