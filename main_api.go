package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func runAPIServer() {
	// 1. Configurazione Iniziale
	dataDir := "data"
	if len(os.Args) > 1 {
		dataDir = os.Args[1]
	}

	fmt.Println("🌐 --- PEP PRODUCTION API BRIDGE v1.1 ---")
	fmt.Println("🔒 Modalità PRODUZIONE: Validazione crittografica obbligatoria.")
	fmt.Println("📊 Endpoint: balance, submit, rooms, messages, agreements, reputation, escrow")

	store := NewStore(dataDir)
	engine := NewEngine(store)
	defer engine.Close()

	// 2. Helper per risposte JSON
	jsonResponse := func(w http.ResponseWriter, data interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(data)
	}

	// 3. Middleware CORS
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	// 4. REGISTRAZIONE ENDPOINT API (PRIMA del file server!)

	// Endpoint 1: Saldo
	http.HandleFunc("/api/v1/balance", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "ID mancante"})
			return
		}
		ledger, reserved := engine.GetBalance(id)
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": "Saldo recuperato",
			"data": map[string]interface{}{
				"id":        id,
				"ledger":    ledger,
				"reserved":  reserved,
				"available": ledger - reserved,
			},
		})
	}))

	// Endpoint 2: Submit
	http.HandleFunc("/api/v1/submit", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Event Event `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "JSON non valido"})
			return
		}

		if err := engine.ProcessEvent(req.Event); err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "message": err.Error()})
			return
		}

		jsonResponse(w, map[string]interface{}{"success": true, "message": "Evento processato con successo"})
	}))

	// Endpoint 3: Rooms
	http.HandleFunc("/api/v1/rooms", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		rooms := engine.GetRooms()
		roomList := make([]Room, 0, len(rooms))
		for _, r := range rooms {
			roomList = append(roomList, *r)
		}
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d rooms trovate", len(roomList)),
			"data":    roomList,
		})
	}))

	// Endpoint 4: Messaggi
	http.HandleFunc("/api/v1/messages", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		userID := r.URL.Query().Get("id")
		if userID == "" {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "Parametro 'id' mancante"})
			return
		}
		msgs := engine.GetMessages(userID)
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d messaggi trovati", len(msgs)),
			"data":    msgs,
		})
	}))

	// Endpoint 5: Accordi
	http.HandleFunc("/api/v1/agreements", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		userID := r.URL.Query().Get("id")
		if userID == "" {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "Parametro 'id' mancante"})
			return
		}
		agreements := engine.GetAgreements(userID)
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d accordi trovati", len(agreements)),
			"data":    agreements,
		})
	}))

	// Endpoint 6: Reputazione (NUOVO - v1.1)
	http.HandleFunc("/api/v1/reputation", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		userID := r.URL.Query().Get("id")
		if userID == "" {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "Parametro 'id' mancante"})
			return
		}
		rep := engine.GetReputation(userID)
		maxAmount := engine.MaxTransactionAmount(userID)
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Reputazione calcolata per %s", userID[:16]),
			"data": map[string]interface{}{
				"user_id":             rep.UserID,
				"completed_sales":     rep.CompletedSales,
				"completed_purchases": rep.CompletedPurchases,
				"expired_payments":    rep.ExpiredPayments,
				"disputes_lost":       rep.DisputesLost,
				"score":               rep.Score,
				"trust_level":         rep.TrustLevel,
				"max_transaction":     maxAmount,
			},
		})
	}))

	// Endpoint 7: Escrow (NUOVO - v1.1)
	http.HandleFunc("/api/v1/escrow", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		escrowID := r.URL.Query().Get("id")
		if escrowID == "" {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "Parametro 'id' mancante"})
			return
		}
		escrow, err := engine.GetEscrow(escrowID)
		if err != nil {
			jsonResponse(w, map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Escrow %s trovato", escrowID[:16]),
			"data":    escrow,
		})
	}))

	// Endpoint 8: Transazioni (storico utente)
	http.HandleFunc("/api/v1/transactions", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		userID := r.URL.Query().Get("id")
		if userID == "" {
			jsonResponse(w, map[string]interface{}{"success": false, "message": "Parametro 'id' mancante"})
			return
		}
		txs := engine.GetTransactions(userID)
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d transazioni trovate", len(txs)),
			"data":    txs,
		})
	}))

	// Endpoint 9: Rooms con reputazione
	http.HandleFunc("/api/v1/rooms-with-reputation", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		rooms := engine.GetRoomsWithReputation()
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d rooms trovate", len(rooms)),
			"data":    rooms,
		})
	}))

	// Endpoint 10: Sync Header
	http.HandleFunc("/api/v1/sync/header", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		header := engine.SyncHeader()
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": "Header sincronizzazione",
			"data":    header,
		})
	}))

	// Endpoint 11: Sync Events
	http.HandleFunc("/api/v1/sync/events", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Metodo non consentito", http.StatusMethodNotAllowed)
			return
		}
		fromID := r.URL.Query().Get("from")
		limitStr := r.URL.Query().Get("limit")
		limit := 100
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil {
				limit = l
			}
		}
		events := engine.SyncEvents(fromID, limit)
		jsonResponse(w, map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%d eventi sincronizzati", len(events)),
			"data":    events,
		})
	}))

	// 5. File Server per la Web App (gestisce solo le richieste non-API)
	fileServer := http.FileServer(http.Dir("."))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Se la richiesta è per un'API, ignora (già gestita sopra)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			return
		}
		// Altrimenti servi il file statico
		fileServer.ServeHTTP(w, r)
	})

	// 6. Avvio Server
	port := 8080
	fmt.Printf("✅ Server in ascolto su http://localhost:%d\n", port)
	fmt.Printf(" Web App disponibile su http://localhost:%d/index.html\n", port)
	fmt.Printf(" API pronte per la Web App\n")

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}