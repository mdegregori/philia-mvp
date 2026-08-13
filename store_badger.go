package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort" // <-- AGGIUNTO per ordinare il replay

	"github.com/dgraph-io/badger/v4"
)

// Store rappresenta il livello di persistenza su BadgerDB
type Store struct {
	db *badger.DB
	// Rimosso dataMu: Badger è già thread-safe internamente
}

// NewStore apre o crea il database Badger nella directory dataDir
func NewStore(dataDir string) (*Store, error) {
	opts := badger.DefaultOptions(dataDir).
		WithLogger(nil) // Disabilita il logger verbose di default

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("errore apertura BadgerDB: %w", err)
	}

	return &Store{db: db}, nil
}

// Close chiude il database
func (s *Store) Close() error {
	return s.db.Close()
}

// AppendEvent salva un evento nel DB usando la chiave "evt:<ID>"
func (s *Store) AppendEvent(ev Event) error {
	key := []byte("evt:" + ev.ID)

	value, err := json.Marshal(ev)
		if err != nil {
		return fmt.Errorf("errore marshaling evento: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
}

// LoadEvents carica tutti gli eventi dal DB
func (s *Store) LoadEvents() ([]Event, error) {
	var events []Event

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("evt:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			value, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			var ev Event
			if err := json.Unmarshal(value, &ev); err != nil {
				log.Printf("⚠️ Errore parsing evento %s: %v", string(item.Key()), err)
				continue
			}
			events = append(events, ev)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("errore lettura eventi: %w", err)
	}

	// 🛡️ FIX CRITICO: Ordiniamo gli eventi per timestamp per garantire 
	// un replay cronologico corretto ed evitare che finiscano nell'orphan pool.
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})

	return events, nil
}

// SaveSnapshot salva una mappa di stati (balances, reserved) come JSON atomico
func (s *Store) SaveSnapshot(balances, reserved map[string]int64) error {
	snapshot := struct {
		Balances map[string]int64 `json:"balances"`
		Reserved map[string]int64 `json:"reserved"`
	}{
		Balances: balances,
		Reserved: reserved,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("snapshot"), data)
	})
}

// LoadSnapshot carica balances e reserved dal DB
func (s *Store) LoadSnapshot() (map[string]int64, map[string]int64, error) {
	var balances, reserved map[string]int64

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("snapshot"))
		if err == badger.ErrKeyNotFound {
			// Nessuno snapshot: inizializza mappe vuote
			balances = make(map[string]int64)
			reserved = make(map[string]int64)
			return nil
		}
		if err != nil {
			return err
		}

		value, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}

		var snap struct {
			Balances map[string]int64 `json:"balances"`
			Reserved map[string]int64 `json:"reserved"`
		}
		if err := json.Unmarshal(value, &snap); err != nil {
			return err
		}

		balances = snap.Balances
		reserved = snap.Reserved
		return nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("errore caricamento snapshot: %w", err)
	}

	return balances, reserved, nil
}

// DeleteEvent rimuove un evento dal DB (utile per test o recovery)
func (s *Store) DeleteEvent(id string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte("evt:" + id))
	})
}

// GetEvent recupera un evento per ID
func (s *Store) GetEvent(id string) (Event, error) {
	var ev Event
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("evt:" + id))
		if err != nil {
			return err
		}
		value, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		return json.Unmarshal(value, &ev)
	})
	return ev, err
}