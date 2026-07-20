//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/skip2/go-qrcode"
)

// PaymentRequest è la struttura che Bob mette nel QR Code
type PaymentRequest struct {
	Version   int    `json:"v"`
	Type      string `json:"type"`
	Recipient string `json:"recipient"` // ID di Bob
	Amount    int64  `json:"amount"`
	Memo      string `json:"memo"`
	Nonce     string `json:"nonce"` // ID univoco per evitare duplicati
}

func main() {
	if len(os.Args) != 5 {
		fmt.Println("Uso: go run main_qr_gen.go <ID_Bob> <importo> <memo> <nome_file_output.png>")
		fmt.Println("Esempio: go run main_qr_gen.go bob1234567890123456789012345678901234567890123456789012345678 300 Cena bob_qr.png")
		return
	}

	recipient := os.Args[1]
	amount := parseInt(os.Args[2])
	memo := os.Args[3]
	outputFile := os.Args[4]

	// Crea la richiesta di pagamento
	req := PaymentRequest{
		Version:   1,
		Type:      "REQUEST",
		Recipient: recipient,
		Amount:    amount,
		Memo:      memo,
		Nonce:     generateNonce(), // Genera un ID univoco
	}

	// Converti in JSON
	jsonData, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("❌ Errore serializzazione: %v\n", err)
		return
	}

	fmt.Println("📱 --- APP DI BOB: Generazione QR Code ---")
	fmt.Printf("Richiesta di pagamento:\n")
	fmt.Printf("  Destinatario (Bob): %s...\n", recipient[:16])
	fmt.Printf("  Importo: %d Philia\n", amount)
	fmt.Printf("  Memo: %s\n", memo)
	fmt.Printf("  Nonce: %s\n", req.Nonce)

	// Genera il QR Code
	err = qrcode.WriteFile(string(jsonData), qrcode.Medium, 256, outputFile)
	if err != nil {
		fmt.Printf("❌ Errore generazione QR: %v\n", err)
		return
	}

	fmt.Printf("\n✅ QR Code generato con successo: %s\n", outputFile)
	fmt.Println("📲 Bob mostra questo QR Code ad Alice.")
}

func parseInt(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

func generateNonce() string {
	// In un'app reale, useresti crypto/rand per un nonce sicuro
	return fmt.Sprintf("%d", os.Getpid())
}