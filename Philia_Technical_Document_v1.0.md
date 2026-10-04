# Philia Economic Protocol (PEP)

## Technical Architecture and Vision Document

**Version**: 1.0  
**Date**: October 3, 2026  
**Author**: Marco De Gregori  
**Status**: Technical Draft  

🇮🇹 [Italian version](Philia_Documento_Tecnico_v1.0.it.md)

---

## 1. Abstract

Philia is a decentralized economic protocol designed as a *Super App* for financial inclusion and certified communication in low or no Internet connectivity contexts. The system unifies, in a single peer-to-peer (P2P) architecture, four historically separate functions: end-to-end encrypted (E2E) messaging with legal value, value transfer on a Directed Acyclic Graph (DAG), decentralized marketplace based on Rooms, and multi-signature escrow contracts.

The protocol is primarily designed for the populations of developing countries in sub-Saharan Africa, Southeast Asia, and Latin America, where traditional banking infrastructure is absent or inefficient and Internet connectivity is intermittent or non-existent. Philia operates on self-organizing mesh networks, ensuring operational continuity even in the absence of an Internet backbone, and provides a credit distribution model through a network of convenience stores that act as distributed branches of the protocol.

The architectural *unicum* of Philia lies in the concept of **Certified Message**: a cryptographically signed event, temporally anchored to the DAG and non-repudiable, which assumes the value of decentralized legal proof, functionally equivalent to a PEC (Certified Electronic Mail) but without a central certification authority.

---

## 2. Vision and Mission

### 2.1 The Problem

According to the World Bank's Global Findex Database (2024), approximately 1.4 billion adults worldwide are *unbanked*, meaning they lack access to formal financial services. The McKinsey Global Payments Report 2025 highlights how cross-border payment flows to emerging economies are growing at sustained rates, but are still burdened by average fees of 6-9% and settlement times of 2-5 business days.

In contexts of political instability, hyperinflation, or absence of the rule of law, local currencies lose their function as a store of value, and centralized payment systems become vehicles for surveillance and censorship.

### 2.2 The Philia Solution

Philia proposes a *sovereign-by-design* architecture, in which:

- **Identity is self-sovereign (SSI)**, based on cryptographic key pairs generated locally.
- **Value is represented by an internal unit of account (Philia)**, issued via genesis events and transferred on the DAG.
- **Trust is computational and reputational**, not institutional: every transaction is verifiable by any node in the network through the causal structure of the DAG.
- **Communication is certified and non-repudiable**, with E2E encryption guaranteeing privacy even against relay nodes.

---

## 3. Technical Architecture

### 3.1 Technology Stack

| Component | Technology | Justification |
| :--- | :--- | :--- |
| **Language** | Go 1.22+ | Native concurrency (goroutines), static compilation, cross-platform portability |
| **P2P Networking** | libp2p | Modular stack for discovery, routing, and multi-protocol transport |
| **Local Database** | BadgerDB v4 | Embedded key-value store, ACID-compliant, optimized for SSDs |
| **Compression** | Snappy (native in BadgerDB) | 60-70% disk footprint reduction for mobile devices |
| **Digital Signature** | Ed25519 (RFC 8032) | Fast, deterministic signature, resistant to side-channel attacks |
| **Key Exchange** | X25519 (Curve25519) | Elliptic curve Diffie-Hellman for E2E |
| **E2E Encryption** | AES-256-GCM | Authentication + confidentiality in a single operation |
| **Data Structure** | DAG with CLO ordering | Causal Lexicographic Ordering for deterministic consensus |

### 3.2 The DAG and CLO Consensus

The heart of the protocol is a Directed Acyclic Graph (DAG) of signed events. Unlike linear blockchains (Bitcoin, Ethereum), Philia's DAG allows parallel convergence of multiple causal branches, reducing confirmation latency and energy consumption.

Each event is an immutable struct containing: type, parent hash, sender, recipient, amount, timestamp, nonce, TTL, memo, status, public key, and signature.

#### 3.2.1 Causal Lexicographic Ordering (CLO)

Consensus on which event takes precedence in case of conflict (e.g., double spend) is determined by the CLO algorithm, which operates in two deterministic phases:

1. **Phase 1 — Nonce comparison**: The monotonic nonce per identity prevents replay attacks and provides a verifiable logical temporal ordering.
2. **Phase 2 — Lexicographic tie-breaking**: In case of identical nonces (concurrent events), the SHA-256 hash of the event ID acts as a deterministic random beacon, guaranteeing that all honest nodes converge on the same winner.

This approach eliminates the need for Proof-of-Work, reducing energy consumption to levels compatible with low-power mobile devices.

### 3.3 Cryptography and Security

#### 3.3.1 Identity and Signature

Each identity is an Ed25519 key pair. The public key serves as a universal address in the system. Every event is signed with Ed25519, guaranteeing:

- **Authenticity**: only the holder of the private key can generate the event.
- **Integrity**: any alteration of the payload invalidates the signature.
- **Non-repudiation**: the signer cannot deny authorship of the event.

#### 3.3.2 E2E Messaging

For private messaging, Philia implements a protocol inspired by the Signal Protocol:

1. The sender retrieves the recipient's X25519 key from the DAG (KEY_ANNOUNCE event).
2. A shared secret is computed via Diffie-Hellman on Curve25519.
3. The message is encrypted with AES-256-GCM, producing authenticated ciphertext.
4. The ciphertext is inserted into the Memo field of a MESSAGE event and signed with Ed25519.

No node in the network, including relays, can decrypt the content. The only public information is the sender-recipient pair and the timestamp.

#### 3.3.3 Anti-Replay and Double Spend

- **Monotonic nonce**: each identity maintains an incremental counter; events with non-increasing nonces are rejected.
- **Idempotency**: events with an ID already present in the DAG are discarded.
- **Conflict resolution**: CLO deterministically invalidates the loser in case of double spend.

#### 3.3.4 Time-To-Live (TTL)

Payments have a configurable TTL (default: 7 days). If a PAYMENT is not settled within the TTL, the reserved funds are automatically unlocked and returned to the sender, with a reputational penalty.

### 3.4 On-DAG Reputation System

Reputation is computed deterministically by each node by analyzing the DAG, according to the formula:

**Score = (Completed Sales × 10) + (Completed Purchases × 2) − (Expired Payments × 5) − (Lost Disputes × 20)**

Trust levels are:

| Level | Score | Transaction Limit |
| :--- | :--- | :--- |
| `UNTRUSTED` | ≤ 0 | Escrow transactions only |
| `NEW` | 1-9 | 100 Philia |
| `ESTABLISHED` | 10-49 | 1,000 Philia |
| `TRUSTED` | 50-99 | 10,000 Philia |
| `VERIFIED` | ≥ 100 | 100,000 Philia |

Reputation is **non-transferable** and **non-manipulable**, being derived exclusively from signed and verifiable historical events.

---

## 4. Functional Components

### 4.1 Certified Messaging (Unique Feature)

The Certified Message is the distinctive element of Philia. It combines:

- E2E encryption (content privacy).
- Ed25519 signature (authenticity and non-repudiation).
- DAG anchoring (immutable and verifiable timestamp).
- Distributed persistence (censorship resistance).

The result is a message that has the value of **decentralized legal proof**, functionally equivalent to a PEC but without dependence on a central operator (such as Poste Italiane for traditional PEC).

**Use cases**:
- Private contracts.
- Legal notices.
- Official communications in contexts lacking the rule of law.
- Tracking of commercial agreements.

### 4.2 Payment System

Payment in Philia follows a two-phase model inspired by traditional financial protocols:

1. **RESERVATION (`PAYMENT`)**: funds are locked (`reserved`) on the sender's ledger but not yet transferred.
2. **SETTLEMENT (`SETTLE`)**: the recipient confirms the service and funds are actually transferred.

This model prevents fraud and provides a basis for the escrow mechanism.

### 4.3 Rooms: Decentralized Marketplace

Rooms are public or private showcases where a user can display goods or services. Each Room is a signed and DAG-anchored `ROOM_CREATE` event containing:

- Name, description, category.
- Base price in Philia.
- Visibility (public/private).
- Owner ID.

The `room-info` and `buy-escrow` commands allow the buyer to:

1. View the Room details and the seller's reputation.
2. Initiate a secure purchase via automated escrow, without the need to manually enter IDs or amounts.

### 4.4 Multi-Signature Escrow

For transactions between strangers, Philia implements a multi-signature escrow system:

- The buyer locks funds in an `ESCROW_LOCK` contract.
- N signatures (typically 2) are required to release funds to the seller (`ESCROW_RELEASE`).
- In case of dispute, a third party (arbitrator) can be called to resolve (`ESCROW_DISPUTE`).

The escrow is time-limited (default: 7 days) and cryptographically bound to the reference Room.

### 4.5 POS and E-Commerce Features

Philia is designed to be integrated as:

- **POS (Point of Sale)**: a merchant can generate a QR code containing a pre-signed PAYMENT event, which the customer signs and transmits.
- **E-Commerce**: an HTTP gateway can translate web payment requests into Philia events, allowing online merchants to accept Philia as a payment method.

The event-based DAG architecture allows integration with external systems through webhooks and listeners on SETTLE events.

5. Distribution Strategy: Mesh Networks and Convenience Stores
5.1 Offline Operation via Mesh Networking
In target contexts (rural areas of sub-Saharan Africa, conflict zones, remote areas of Asia and South America), Internet connectivity is intermittent or absent. Philia implements a mesh transport layer based on:
Bluetooth Low Energy (BLE) for short-range communications (up to 100m).
Wi-Fi Direct for medium-range communications (up to 200m).
LoRa (under study) for long-range, low-bandwidth communications (up to 10km).
The libp2p gossip protocol allows epidemic propagation of events: even if a node is isolated, events are stored locally and synchronized as soon as connectivity is restored with a connected peer.
The DAG guarantees that, at the moment of synchronization, the CLO ordering produces the same final state on all nodes, regardless of the order in which events were received.
5.2 Convenience Stores as Distributed Branches
To solve the onboarding problem for unbanked users, Philia provides a network of convenience stores (grocery stores, kiosks, pharmacies) that act as distributed branches of the protocol:
Credit loading: a user hands cash to the store; the store generates a GENESIS event in favor of the user, debiting its own Philia account.
Credit unloading: the user requests cash; the store generates a PAYMENT event in its own favor, debiting the user's account.
Fees: the store retains a configurable commission (typically 1-3%) for the service.
This model:
Eliminates the need for centralized KYC.
Allows entry into the system without technical skills.
Creates an economic incentive for capillary network diffusion.
Stores are themselves Philia nodes, with public and verifiable reputation, and are subject to the same consensus rules as the protocol.
6. Sources of Inspiration and References
The Philia project arises from the convergence of various research lines and market analyses:
6.1 Pasqualini's Thesis
The thesis on decentralized systems provided the initial theoretical framework on the feasibility of an economic protocol without a central authority, with particular attention to DAG-based consensus models.
6.2 McKinsey Global Payments Report 2025
The McKinsey report highlighted the structural inefficiencies of cross-border payment systems to emerging economies, providing the economic motivation for a low-cost decentralized alternative.
6.3 Bitcoin White Paper (Nakamoto, 2008)
The concept of a distributed ledger without a central authority is the theoretical foundation of Philia. However, Philia abandons the linear blockchain model in favor of a DAG, for reasons of energy efficiency and latency.
6.4 IOTA Tangle and DAG-based Ledgers
IOTA's DAG architecture inspired the CLO consensus model, with the goal of eliminating transaction fees for micro-payments.
6.5 Signal Protocol (Open Whisper Systems)
Signal's E2E messaging protocol provided the model for implementing X25519 + AES-GCM encryption used in Philia's private messages.
6.6 Briar and GoTenna
The Briar application (open-source mesh messaging) and GoTenna (off-grid communication hardware) inspired Philia's mesh transport layer, adapted to the specific needs of target contexts.
6.7 World Bank Global Findex Database
Data on the unbanked population guided UX and distribution choices (convenience stores) to maximize system accessibility.
6.8 libp2p Specification
The libp2p specification provided the modular networking architecture on which Philia's P2P layer is built.
7. Roadmap and Future Developments
7.1 Current Phase (v1.10)
Working protocol core on desktop.
Operational E2E messaging.
Two-phase payment system (PAYMENT/SETTLE).
Rooms and automated purchases via escrow.
Snappy compression and BadgerDB optimizations.
7.2 Next Steps
Version
Milestone
v1.11
Implementation of real arbitrator in escrow (three-node test).
v1.12
Delivery receipt (MESSAGE_ACK) for certified messages.
v1.13
Android porting via Go-Mobile cross-compilation.
v1.14
Integration of mesh transport (BLE + Wi-Fi Direct).
v2.0
Test network with convenience stores in a pilot context (Kenya or Colombia).
8. Conclusions
Philia is not a cryptocurrency, nor a simple messenger, nor a marketplace. It is a sovereign economic protocol that unifies communication, value, and trust in a single decentralized architecture, designed to operate even in the most hostile contexts.
Its strength lies in the architectural simplicity (DAG + modern cryptography + libp2p) and in the deep understanding of target contexts, derived from the analysis of McKinsey reports and World Bank data.
Philia is an open-source project, developed in Go, and its code is available on GitHub for verification and contribution.
Full bibliographic references available upon request.
Document drafted on October 3, 2026.
👤 Author: Marco De Gregori
📄 Document: Philia Economic Protocol — Technical Architecture and Vision Document v1.0
🔗 GitHub: @mdegregori
