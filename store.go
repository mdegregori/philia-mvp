package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	dataDir string
}

func NewStore(dataDir string) *Store {
	os.MkdirAll(dataDir, 0755)
	return &Store{dataDir: dataDir}
}

func (s *Store) AppendEvent(ev Event) error {
	path := filepath.Join(s.dataDir, "events.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	_, err = f.WriteString(string(data) + "\n")
	return err
}

func (s *Store) LoadEvents() ([]Event, error) {
	path := filepath.Join(s.dataDir, "events.log")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, scanner.Err()
}

func (s *Store) SaveSnapshot(balances, reserved map[string]int64) {
	path := filepath.Join(s.dataDir, "snapshot.json")
	data := map[string]any{
		"balances": balances,
		"reserved": reserved,
	}
	jsonData, _ := json.Marshal(data)
	os.WriteFile(path, jsonData, 0644)
	fmt.Println("💾 Snapshot salvato.")
}