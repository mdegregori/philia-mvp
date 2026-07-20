//go:build ignore

package main

import (
    "context"
    "crypto/ed25519"
    "encoding/hex" // <--- MANCAVA QUESTO
    "encoding/json"
    "fmt"
    "os"

    libp2p "github.com/libp2p/go-libp2p"
    pubsub "github.com/libp2p/go-libp2p-pubsub"
    host "github.com/libp2p/go-libp2p/core/host"
    peer "github.com/libp2p/go-libp2p/core/peer"
    "github.com/multiformats/go-multiaddr"
)

// ----------------------------
// P2P NODE
// ----------------------------

type P2PNode struct {
    ctx     context.Context
    host    host.Host
    ps      *pubsub.PubSub
    topic   *pubsub.Topic
    sub     *pubsub.Subscription

    engine *Engine

    privKey ed25519.PrivateKey
    pubKey  ed25519.PublicKey
    nodeID  string
}

// ----------------------------
// CREAZIONE NODO
// ----------------------------

func NewP2PNode(ctx context.Context, engine *Engine) (*P2PNode, error) {

    // 🔐 Carica o crea chiavi NETWORK (Identità P2P)
    pub, priv, err := loadOrCreateKey("data/node.key")
    if err != nil {
        return nil, err
    }

    // CORREZIONE QUI: Converti la chiave pubblica in Stringa Hex per l'ID
    nodeID := PubKeyToID(hex.EncodeToString(pub))

    // 🌐 Host libp2p
    h, err := libp2p.New()
    if err != nil {
        return nil, err
    }

    // 📡 PubSub Gossip
    ps, err := pubsub.NewGossipSub(ctx, h)
    if err != nil {
        return nil, err
    }

    topic, err := ps.Join("philia-events")
    if err != nil {
        return nil, err
    }

    sub, err := topic.Subscribe()
    if err != nil {
        return nil, err
    }

    fmt.Println("Node started:", h.ID())
    fmt.Println("Node logical ID:", nodeID)

    return &P2PNode{
        ctx:     ctx,
        host:    h,
        ps:      ps,
        topic:   topic,
        sub:     sub,
        engine:  engine,
        privKey: priv,
        pubKey:  pub,
        nodeID:  nodeID,
    }, nil
}

// ----------------------------
// ADDRESSES
// ----------------------------

func (n *P2PNode) HostAddrs() []string {
    addrs := []string{}
    for _, a := range n.host.Addrs() {
        addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", a, n.host.ID().String()))
    }
    return addrs
}

// ----------------------------
// CONNECT
// ----------------------------

func (n *P2PNode) ConnectToPeer(addr string) error {
    maddr, err := multiaddr.NewMultiaddr(addr)
    if err != nil {
        return err
    }

    pi, err := peer.AddrInfoFromP2pAddr(maddr)
    if err != nil {
        return err
    }

    return n.host.Connect(n.ctx, *pi)
}

// ----------------------------
// LISTEN
// ----------------------------

func (n *P2PNode) StartListening() {
    go func() {
        for {
            msg, err := n.sub.Next(n.ctx)
            if err != nil {
                continue
            }

            var event Event
            err = json.Unmarshal(msg.Data, &event)
            if err != nil {
                continue
            }

            err = n.engine.ProcessEvent(event)
            if err != nil {
                fmt.Println("❌ Errore applicazione evento:", err)
            }
        }
    }()
}

// ----------------------------
// PUBLISH
// ----------------------------

func (n *P2PNode) Publish(event Event) error {

    // 🚀 IMPORTANTE: NON RIFIRMA QUI.
    // L'evento deve essere già firmato da Main.go (Alice) con la chiave del Wallet.
    // Se lo rifirmi con la chiave del Nodo, distruggi la prova di proprietà.

    data, err := json.Marshal(event)
    if err != nil {
        return err
    }

    return n.topic.Publish(n.ctx, data)
}

// ----------------------------
// KEY STORAGE
// ----------------------------

func loadOrCreateKey(path string) (ed25519.PublicKey, ed25519.PrivateKey, error) {

    os.MkdirAll("data", 0700)

    data, err := os.ReadFile(path)
    if err == nil {
        priv := ed25519.PrivateKey(data)
        pub := priv.Public().(ed25519.PublicKey)
        return pub, priv, nil
    }

    pub, priv, err := ed25519.GenerateKey(nil)
    if err != nil {
        return nil, nil, err
    }

    err = os.WriteFile(path, priv, 0600)
    if err != nil {
        return nil, nil, err
    }

    return pub, priv, nil
}