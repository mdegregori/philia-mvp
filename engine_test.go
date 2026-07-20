package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

// helper per creare e firmare un evento velocemente nei test
func createAndSignEvent(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey, evType, parent, recipient string, amount int64, reference string) Event {
	myID := hex.EncodeToString(pub)
	ev := NewEvent(evType, parent, myID, recipient, amount, time.Now().UnixNano(), reference)
	ev.Sign(priv)
	return ev
}

func TestEngine_HappyPath(t *testing.T) {
	// 1. ARRANGE: Preparazione
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}
	
	engine := NewEngine()
	myID := hex.EncodeToString(pub)

	// 2. ACT & ASSERT: GENESIS (Mint 1000 Philia)
	gen := createAndSignEvent(t, priv, pub, GENESIS, "", "", 1000, "")
	if err := engine.ProcessEvent(gen); err != nil {
		t.Fatalf("GENESIS failed: %v", err)
	}

	ledger, reserved := engine.GetBalance(myID)
	if ledger != 1000 || reserved != 0 {
		t.Errorf("After GENESIS, expected Ledger=1000, Reserved=0. Got Ledger=%d, Reserved=%d", ledger, reserved)
	}

	// 3. ACT & ASSERT: PAYMENT (Prenota 300 Philia)
	pay := createAndSignEvent(t, priv, pub, PAYMENT, gen.ID, "bob", 300, "")
	if err := engine.ProcessEvent(pay); err != nil {
		t.Fatalf("PAYMENT failed: %v", err)
	}

	ledger, reserved = engine.GetBalance(myID)
	if ledger != 1000 || reserved != 300 {
		t.Errorf("After PAYMENT, expected Ledger=1000, Reserved=300. Got Ledger=%d, Reserved=%d", ledger, reserved)
	}

	// 4. ACT & ASSERT: SETTLE (Conferma il trasferimento di 300 a bob)
	settle := createAndSignEvent(t, priv, pub, SETTLE, pay.ID, "bob", 300, pay.ID)
	if err := engine.ProcessEvent(settle); err != nil {
		t.Fatalf("SETTLE failed: %v", err)
	}

	ledger, reserved = engine.GetBalance(myID)
	bobLedger, _ := engine.GetBalance("bob")

	if ledger != 700 {
		t.Errorf("After SETTLE, sender Ledger should be 700, got %d", ledger)
	}
	if reserved != 0 {
		t.Errorf("After SETTLE, sender Reserved should be 0, got %d", reserved)
	}
	if bobLedger != 300 {
		t.Errorf("After SETTLE, recipient Ledger should be 300, got %d", bobLedger)
	}
}

func TestEngine_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		setupGenesis  int64
		paymentAmount int64
		settleAmount  int64
		expectPayErr  bool
		expectSetErr  bool
	}{
		{
			name:          "Fondi insufficienti per la prenotazione",
			setupGenesis:  100,
			paymentAmount: 200, // Chiede più di quanto ha
			settleAmount:  0,
			expectPayErr:  true,
			expectSetErr:  false,
		},
		{
			name:          "Settle superiore al prenotato",
			setupGenesis:  100,
			paymentAmount: 50,  // Prenota 50
			settleAmount:  100, // Tenta di confermarne 100
			expectPayErr:  false,
			expectSetErr:  true, // Deve fallire qui
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pub, priv, _ := ed25519.GenerateKey(rand.Reader)
			engine := NewEngine()

			// Setup GENESIS
			gen := createAndSignEvent(t, priv, pub, GENESIS, "", "", tt.setupGenesis, "")
			engine.ProcessEvent(gen)

			// Tentativo PAYMENT
			pay := createAndSignEvent(t, priv, pub, PAYMENT, gen.ID, "alice", tt.paymentAmount, "")
			errPay := engine.ProcessEvent(pay)

			if tt.expectPayErr && errPay == nil {
				t.Errorf("Expected PAYMENT error, got nil")
			}
			if !tt.expectPayErr && errPay != nil {
				t.Errorf("Unexpected PAYMENT error: %v", errPay)
			}

			// Se il payment è andato a buon fine, proviamo il SETTLE
			if !tt.expectPayErr {
				settle := createAndSignEvent(t, priv, pub, SETTLE, pay.ID, "alice", tt.settleAmount, pay.ID)
				errSettle := engine.ProcessEvent(settle)

				if tt.expectSetErr && errSettle == nil {
					t.Errorf("Expected SETTLE error, got nil")
				}
				if !tt.expectSetErr && errSettle != nil {
					t.Errorf("Unexpected SETTLE error: %v", errSettle)
				}
			}
		})
	}
}
func TestEngine_EdgeCases(t *testing.T) {
	t.Run("Importo negativo o zero nella prenotazione", func(t *testing.T) {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		engine := NewEngine()
		
		// 1. GENESIS
		gen := createAndSignEvent(t, priv, pub, GENESIS, "", "", 100, "")
		engine.ProcessEvent(gen)

		// 2. PAYMENT con importo negativo
		payNeg := createAndSignEvent(t, priv, pub, PAYMENT, gen.ID, "alice", -50, "")
		errNeg := engine.ProcessEvent(payNeg)
		if errNeg == nil {
			t.Errorf("Expected error for negative amount, got nil")
		}

		// 3. PAYMENT con importo zero
		payZero := createAndSignEvent(t, priv, pub, PAYMENT, gen.ID, "alice", 0, "")
		errZero := engine.ProcessEvent(payZero)
		if errZero == nil {
			t.Errorf("Expected error for zero amount, got nil")
		}
	})

	t.Run("Settle senza Payment precedente", func(t *testing.T) {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		engine := NewEngine()
		
		// 1. GENESIS
		gen := createAndSignEvent(t, priv, pub, GENESIS, "", "", 100, "")
		engine.ProcessEvent(gen)

		// 2. SETTLE diretto (senza aver mai fatto un PAYMENT valido)
		// Usiamo gen.ID come parent, ma non c'è nessun fondo prenotato
		settle := createAndSignEvent(t, priv, pub, SETTLE, gen.ID, "alice", 50, "fake_ref")
		err := engine.ProcessEvent(settle)
		
		if err == nil {
			t.Errorf("Expected error for SETTLE without prior reservation, got nil")
		} else if err.Error() != "not enough reserved balance (Reserved: 0)" {
			t.Errorf("Expected 'not enough reserved balance' error, got: %v", err)
		}
	})

	t.Run("Firma invalida o manomessa", func(t *testing.T) {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		engine := NewEngine()
		
		gen := createAndSignEvent(t, priv, pub, GENESIS, "", "", 100, "")
		
		// Manomettiamo la firma dopo la creazione (simulazione di attacco/corruzione)
		gen.Signature = "firma_manomessa_12345"
		
		err := engine.ProcessEvent(gen)
		if err == nil {
			t.Errorf("Expected signature verification error, got nil")
		}
	})
}