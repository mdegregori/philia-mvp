//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

type PaymentRequest struct {
	Version   int    `json:"v"`
	Type      string `json:"type"`
	Recipient string `json:"recipient"`
	Amount    int64  `json:"amount"`
	Memo      string `json:"memo"`
	Nonce     string `json:"nonce"`
}

var sessionKeys = make(map[string]ed25519.PrivateKey)

func getOrCreateKey(name string) (ed25519.PrivateKey, string) {
	if priv, exists := sessionKeys[name]; exists {
		pub := priv.Public().(ed25519.PublicKey)
		return priv, hex.EncodeToString(pub)
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sessionKeys[name] = priv
	return priv, hex.EncodeToString(pub)
}

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Uso: go run main_qr_scan.go <nome_alice> <file_qr.png>")
		fmt.Println("Esempio: go run main_qr_scan.go alice bob_qr.png")
		return
	}

	aliceName := os.Args[1]
	qrFile := os.Args[2]

	fmt.Println("📱 --- APP DI ALICE: Scansione QR Code ---")

	// 1. Leggi l'immagine del QR Code
	file, err := os.Open(qrFile)
	if err != nil {
		fmt.Printf("❌ Impossibile aprire il file: %v\n", err)
		return
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Printf("❌ Impossibile decodificare l'immagine: %v\n", err)
		return
	}

	// 2. Decodifica il QR Code
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		fmt.Printf("❌ Errore creazione bitmap: %v\n", err)
		return
	}

	qrReader := qrcode.NewQRCodeReader()
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		fmt.Printf("❌ Impossibile leggere il QR Code: %v\n", err)
		return
	}

	fmt.Printf("✅ QR Code scansionato con successo!\n")
	fmt.Printf("   Contenuto grezzo: %s\n", result.GetText()[:50]+"...")

	// 3. Parsa il JSON della richiesta di pagamento
	var req PaymentRequest
	if err := json.Unmarshal([]byte(result.GetText()), &req); err != nil {
		fmt.Printf("❌ Errore parsing JSON: %v\n", err)
		return
	}

	fmt.Printf("\n📋 Dettagli richiesta:\n")
	fmt.Printf("   Destinatario: %s...\n", req.Recipient[:16])
	fmt.Printf("   Importo: %d Philia\n", req.Amount)
	fmt.Printf("   Memo: %s\n", req.Memo)

	// 4. Inizializza il motore PEP
	store := NewStore("data")
	engine := NewEngine(store)
	defer engine.Close()

	// 5. Carica l'identità di Alice
	priv, aliceID := getOrCreateKey(aliceName)
	fmt.Printf("\n🔑 Identità Alice caricata: %s...\n", aliceID[:16])

	// 6. Controlla se Alice ha fondi, altrimenti dalle 1000 Philia (GENESIS)
	ledger, _ := engine.GetBalance(aliceID)
	if ledger == 0 {
		fmt.Println("💰 Alice non ha fondi. Creazione GENESIS automatico...")
		genesis := NewEvent(GENESIS, "", aliceID, "", 1000, time.Now().UnixNano(), "Auto-funding for QR test")
		genesis.Sign(priv)
		if err := engine.ProcessEvent(genesis); err != nil {
			fmt.Printf("❌ Errore funding: %v\n", err)
			return
		}
		fmt.Println("✅ Alice ha ricevuto 1000 Philia (GENESIS)")
	} else {
		fmt.Printf("💰 Alice ha già %d Philia disponibili.\n", ledger)
	}

	// 7. Costruisci l'evento PAYMENT
	parent := engine.GetLastHash()
	ev := NewEvent(PAYMENT, parent, aliceID, req.Recipient, req.Amount, time.Now().UnixNano(), req.Memo)

	// 8. Firma l'evento con la chiave privata di Alice
	ev.Sign(priv)

	// 9. Invia l'evento al motore
	if err := engine.ProcessEvent(ev); err != nil {
		fmt.Printf("❌ Errore elaborazione: %v\n", err)
		return
	}

	fmt.Printf("\n✅ PAGAMENTO REGISTRATO NEL DAG LOCALE!\n")
	fmt.Printf("   Evento ID: %s...\n", ev.ID[:16])
	fmt.Printf("   Alice ha prenotato %d Philia per %s...\n", req.Amount, req.Recipient[:16])

	// 10. Mostra il saldo finale
	finalLedger, finalReserved := engine.GetBalance(aliceID)
	fmt.Printf("\n--- SALDO FINALE DI ALICE ---\n")
	fmt.Printf("Totale (Ledger)   : %d Philia\n", finalLedger)
	fmt.Printf("In prenotazione   : %d Philia\n", finalReserved)
	fmt.Printf("Disponibili       : %d Philia\n", finalLedger-finalReserved)
	fmt.Println("-------------------------------")
	fmt.Println("(Verrà sincronizzato alla prossima connessione P2P)")
}