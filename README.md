# Philia Economic Protocol (PEP) v1.1

A decentralized economic protocol for P2P marketplace with CLO consensus, on-DAG reputation, multi-signature escrow, encrypted chat, and efficient synchronization.

## 🏛️ Overview

PEP is a decentralized economic system combining:
- **DAG (Directed Acyclic Graph)** for distributed consensus
- **On-chain reputation** automatically calculated from transactions
- **Secure payments** with multi-signature Escrow system
- **Decentralized chat** for user negotiations
- **ROOMS Marketplace** for accommodation/service bookings

## 🌟 Key Features

### 1. DAG Consensus with libp2p
- DAG data structure for causal event ordering
- **libp2p** integration for P2P networking (GossipSub for event propagation)
- External blockchain notarization via `anchor.go`
- Append-only disk persistence (`store.go`)

### 2. On-DAG Reputation System
- Automatic score calculation based on:
  - Completed sales (+10 points)
  - Completed purchases (+2 points)
  - Expired payments (-5 points)
  - Lost disputes (-20 points)
- Trust levels: UNTRUSTED → NEW → ESTABLISHED → TRUSTED → VERIFIED
- Visual trust badges in marketplace for seller identification

### 3. Payments with Multi-Signature Escrow
- **Two-phase flow**: PAYMENT (reservation) → SETTLE (final transfer)
- **Secure escrow**: funds locked until delivery confirmation
- **Configurable arbitrator**: dispute resolution with trusted third party
- **Multi-signature**: buyer + seller consensus required for fund release
- **TTL (Time-To-Live)**: automatic expiration of reserved payments

### 4. Decentralized Chat
- Cryptographically signed messages (Ed25519)
- Message types:
  - `MESSAGE`: standard text chat
  - `AGREEMENT`: contractual agreements recorded on DAG
- Automatic polling every 5 seconds for real-time updates
- End-to-end encryption with participant public keys

### 5. ROOMS Marketplace
- Service/accommodation listings with metadata:
  - Name, category, price per night
  - Detailed description
  - Owner ID with reputation badge
- Integrated booking with automatic payment
- P2P network synchronization with seller reputation display

### 6. Efficient DAG Synchronization
- **`/sync/header` endpoint**: returns total_events, last_hash, merkle_root
- **`/sync/events` endpoint**: download events in chunks of 100
- Hash chain integrity verification (each event points to previous)
- Local storage in localStorage to avoid re-downloading
- Automatic sync on startup after wallet import

## 🔐 Security

- **Ed25519 signatures**: every event signed with private key
- **Persistent nonce**: prevents replay attacks (stored in localStorage)
- **Key validation**: private key integrity check on import
- **Hash chain**: verification that each event correctly references the previous
- **Merkle Root**: fast integrity verification of entire DAG

## 🏗️ Project Architecturephilia-mvp/

├── main_api.go # REST API Bridge (HTTP server, CORS, endpoint routing)
├── engine.go # DAG Engine (consensus, reputation, escrow, sync logic)
├── types.go # Data structures (Event, Room, Escrow, ReputationScore)
├── anchor.go # External blockchain notarization
├── store.go # Append-only disk persistence (JSONL)
└── index.html # SuperApp frontend (SPA in vanilla JS)


## ⚙️ Installation & Usage

### Prerequisites
- Go 1.19+
- Modern browser (Chrome, Firefox, Edge)

### Start a Node
```bash
# Build
go build -o pep-api.exe main_api.go anchor.go types.go engine.go store.go

# Run
./pep-api.exe data

Server will be available at http://localhost:8080
Access the Web App
Open http://localhost:8080/index.html
Generate or import a wallet (Ed25519 key)
System synchronizes automatically
Start using the marketplace
🔌 API Endpoints
Wallet & Transactions
GET /api/v1/balance?id=<pubkey> - Available/ledger/reserved balance
POST /api/v1/submit - Submit signed event (PAYMENT, SETTLE, MESSAGE, etc.)
GET /api/v1/transactions?id=<pubkey> - Filterable transaction history
Reputation & Escrow
GET /api/v1/reputation?id=<pubkey> - Score and trust level
GET /api/v1/escrow?id=<escrow_id> - Escrow details
GET /api/v1/rooms-with-reputation - Marketplace with trust badges
Synchronization
GET /api/v1/sync/header - DAG header (total_events, last_hash, merkle_root)
GET /api/v1/sync/events?from=<id>&limit=100 - Download events in chunks
Chat & Marketplace
GET /api/v1/messages?id=<pubkey> - Received messages
GET /api/v1/agreements?id=<pubkey> - Recorded agreements
GET /api/v1/rooms - Available room listings
📊 Supported Event Types
Type
Description
TTL
Usage
PAYMENT
Fund reservation
7 days
Service payment
SETTLE
Final settlement
0 (immediate)
Payment completion
MESSAGE
Chat message
0
User communication
AGREEMENT
Contractual agreement
0
On-DAG contracts
ESCROW_LOCK
Lock escrow funds
0
Open escrow
ESCROW_RELEASE
Release funds
0
Confirm delivery
ESCROW_DISPUTE
Open dispute
0
Request arbitration
ROOM_CREATE
Publish room
0
Marketplace listing
🌐 Roadmap
✅ Completed (v1.1)
DAG Engine with CLO consensus
On-DAG reputation system with trust badges
Multi-signature escrow with configurable arbitrator
DAG synchronization with Merkle Root
Full SPA Web App
Automatic sync on startup
Real-time chat with polling
ROOMS Marketplace with reputation
Append-only disk persistence
External blockchain notarization
🔄 In Progress (v1.2)
Multi-node P2P deployment on VPS
Full GossipSub integration for event propagation
WebSocket for real-time chat (replace polling)
Browser push notifications
Data compression for efficient sync
Bloom filter for optimized sync
🔮 Future (v2.0)
Smart contracts for automated escrow
Tokenomics with native PEP token
Decentralized governance (DAO)
Mobile app (Android/iOS)
🤝 Contributing
This is an open-source project. Contributions welcome:
🐛 Report bugs
💡 Suggest new features
🔀 Submit pull requests
📄 License
MIT License - See LICENSE file for details.
👤 Author
Marco De Gregori
GitHub: @mdegregori
Developed as part of the Philia Economic Protocol - A decentralized economic system for secure, reputation-based P2P transactions without intermediaries.
