# PeerFs

> **Distributed Peer-to-Peer Content-Addressed File Storage System in Go**

PeerFs is a lightweight, decentralized, peer-to-peer (P2P) file system built from scratch in Go. It pairs a **Content-Addressed Storage (CAS)** engine with a custom, framed **TCP wire protocol**, allowing autonomous nodes to store, replicate, and retrieve data blocks across a cooperative distributed mesh without centralized servers or single points of failure.

---

## Table of Contents
- [System Architecture](#system-architecture)
- [Core Architectural Tenets](#core-architectural-tenets)
- [Content-Addressed Storage (CAS) Engine](#content-addressed-storage-cas-engine)
  - [The Flat Directory Bottleneck](#the-flat-directory-bottleneck)
  - [Deterministic SHA-1 Path Sharding](#deterministic-sha-1-path-sharding)
  - [Zero-Buffer Streaming I/O](#zero-buffer-streaming-io)
- [P2P Transport & Wire Protocol](#p2p-transport--wire-protocol)
  - [TCP Transport & Peer Abstraction](#tcp-transport--peer-abstraction)
  - [Peer Handshake & Identity Verification](#peer-handshake--identity-verification)
  - [Message Framing & RPC Decoding](#message-framing--rpc-decoding)
- [Distributed Operations](#distributed-operations)
  - [File Ingestion & Concurrent Peer Replication](#file-ingestion--concurrent-peer-replication)
  - [Remote File Retrieval & Query Routing](#remote-file-retrieval--query-routing)
  - [Cluster Topology & Bootstrap Discovery](#cluster-topology--bootstrap-discovery)
- [HTTP API & CLI Interface](#http-api--cli-interface)
- [Project Layout](#project-layout)
- [Getting Started & Development](#getting-started--development)

---

## System Architecture

PeerFs is organized into four modular layers, isolating network transport from storage semantics and orchestration:

```mermaid
flowchart TD
    subgraph Clients["1. Client Layer"]
        CLI["CLI Tool (cmd/peerfs-cli)"]
        HTTP["HTTP API / REST Endpoints (/files, /peers)"]
    end

    subgraph Core["2. Orchestration Layer (FileServer)"]
        FS["FileServer Daemon"]
        PR["Peer Registry (map[string]Peer with sync.RWMutex)"]
        DL["Event Dispatch Loop (Consume channel)"]
        FS --> PR
        FS --> DL
    end

    subgraph Transport["3. Network Transport Layer (p2p)"]
        TP["TCPTransport (Listener & Connection Pool)"]
        HS["Handshake Protocol (Identity & Version Validation)"]
        DEC["Message Decoder (Wire Framing & RPC Parser)"]
        TP --> HS
        TP --> DEC
    end

    subgraph Storage["4. Storage Engine Layer (storage)"]
        ST["Store Engine"]
        SH["SHA-1 Sharder (PathTransformFunc)"]
        DISK[("Disk Storage Root (cas-sharded dirs)")]
        ST --> SH
        ST --> DISK
    end

    Clients --> Core
    Core --> Transport
    Core --> Storage
```

---

## Core Architectural Tenets

1. **Content Addressing Over Human Naming**: Files are stored and retrieved purely by their cryptographic digest (SHA-1 hash). If two users upload the exact same file, it is automatically deduplicated and stored once.
2. **Zero-Buffer Stream Pipelining**: Large files are never buffered entirely in memory. Data streams directly from the incoming network socket or HTTP request through `io.MultiWriter` or `io.TeeReader` into local disk storage and peer TCP sockets simultaneously.
3. **Self-Healing Distributed Queries**: If a requested block is absent on the local node, the node queries its connected peers, downloads the stream from the first responder, caches it locally in CAS, and serves the caller in a single continuous stream.
4. **Decoupled Transport Protocols**: The transport layer is defined via a clean `Transport` interface, enabling drop-in alternatives (e.g. UDP, QUIC, WebSocket) without altering storage or replication logic.

---

## Content-Addressed Storage (CAS) Engine

### The Flat Directory Bottleneck
Standard operating system file systems (ext4, NTFS, APFS) degrade significantly in performance when tens of thousands of files are placed inside a single directory due to directory lock contention and $O(N)$ or complex B-tree directory traversal overhead.

### Deterministic SHA-1 Path Sharding
PeerFs breaks the cryptographic hash into hierarchical prefix slices to create a balanced, shallow directory tree:

```mermaid
flowchart LR
    File["Original Payload"] -->|SHA-1 Hash| Hash["2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"]
    Hash -->|Slice Prefix 1: 2a| D1["data/2a/"]
    Hash -->|Slice Prefix 2: ae| D2["data/2a/ae/"]
    Hash -->|Slice Prefix 3: 6c| D3["data/2a/ae/6c/"]
    Hash -->|Remaining Hash: 35c9...| Leaf["35c94fcfb415dbe95f408b9ce91ee846ed"]
    D1 --> D2 --> D3 --> Leaf
```

- **Hash Transformation**:
  $$\text{Key} \xrightarrow{\text{SHA-1}} \texttt{2aae6c35c94fcfb415dbe95f408b9ce91ee846ed}$$
  $$\text{Storage Path} = \texttt{data/2a/ae/6c/35c94fcfb415dbe95f408b9ce91ee846ed}$$
- **Fast Lookups**: Directory depth is constant ($O(1)$ lookup time), eliminating inode contention and filesystem slowdowns.

### Zero-Buffer Streaming I/O
The `Store` interface interacts exclusively with `io.Reader` and `io.ReadCloser`:

```go
type Store struct {
    Root              string
    PathTransformFunc PathTransformFunc
}

func (s *Store) Write(key string, r io.Reader) (int64, error)
func (s *Store) Read(key string) (io.ReadCloser, error)
func (s *Store) Has(key string) bool
func (s *Store) Delete(key string) error
```

When writing, bytes pipe directly through `io.Copy(file, reader)` into disk storage, maintaining a steady $O(1)$ memory footprint regardless of file size (from 1 KB to 100 GB).

---

## P2P Transport & Wire Protocol

### TCP Transport & Peer Abstraction
Nodes communicate over bidirectional TCP sockets. The `TCPTransport` manages incoming listeners and outgoing client dials:

```mermaid
classDiagram
    class Transport {
        <<interface>>
        +ListenAndAccept() error
        +Consume() <-chan RPC
        +Close() error
    }
    class Peer {
        <<interface>>
        +net.Conn
        +Send([]byte) error
        +Close() error
    }
    class TCPTransport {
        -listenAddr string
        -listener net.Listener
        -rpcChan chan RPC
        -handshakeFunc HandshakeFunc
        -decoder Decoder
        +ListenAndAccept() error
        +Consume() <-chan RPC
    }
    class TCPPeer {
        -net.Conn
        -outbound bool
        +Send([]byte) error
    }

    Transport <|.. TCPTransport
    Peer <|.. TCPPeer
```

### Peer Handshake & Identity Verification
Before any RPC commands or file transfers are processed, connecting nodes must complete an explicit handshake:

```mermaid
sequenceDiagram
    autonumber
    participant NodeA as Dialing Peer (Node A)
    participant NodeB as Listening Peer (Node B)

    NodeA->>NodeB: TCP Connect
    NodeB->>NodeB: Spawn handleConn(conn) goroutine
    NodeA->>NodeB: Send Handshake Payload (Protocol Version + Node ID)
    NodeB->>NodeB: Execute HandshakeFunc(peer)
    alt Invalid Protocol Version or Node ID
        NodeB-->>NodeA: Terminate Connection (Close)
    else Handshake Valid
        NodeB->>NodeB: Register peer in active peer table
        NodeB->>NodeB: Dispatch to readLoop()
    end
```

### Message Framing & RPC Decoding
Because TCP delivers a continuous stream of bytes without message boundaries, PeerFs employs a framed message structure:

| Field | Size | Description |
| :--- | :--- | :--- |
| **MessageType** | 1 byte | Command code (e.g. `0x01` Store, `0x02` Get, `0x03` Status) |
| **PayloadLength** | 8 bytes (uint64) | Big-endian length of the subsequent payload |
| **StreamFlag** | 1 byte | Indicates whether raw streaming bytes follow |
| **Payload / Stream** | $N$ bytes | Encoded payload or raw binary file stream |

Incoming bytes are parsed by the `Decoder` and pushed onto the `Consume() <-chan RPC` channel for consumption by the `FileServer` dispatch loop.

---

## Distributed Operations

### File Ingestion & Concurrent Peer Replication
When a client uploads a file to Node A, Node A writes to its local CAS storage while simultaneously streaming the payload to all connected peers concurrently:

```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant NodeA as Node A (Coordinator)
    participant LocalDisk as Node A CAS Storage
    participant NodeB as Peer B
    participant NodeC as Peer C

    Client->>NodeA: POST /files (File Stream)
    NodeA->>NodeA: Calculate SHA-1 Hash
    NodeA->>NodeA: Initialize io.MultiWriter(LocalDisk, PeerB, PeerC)
    
    par Concurrent Stream Write
        NodeA->>LocalDisk: Pipe chunks to sharded disk path
    and Stream to Peer B
        NodeA->>NodeB: Stream chunks over TCP (MessageStoreFile)
        NodeB->>NodeB: Write chunks directly to Peer B CAS
    and Stream to Peer C
        NodeA->>NodeC: Stream chunks over TCP (MessageStoreFile)
        NodeC->>NodeC: Write chunks directly to Peer C CAS
    end

    LocalDisk-->>NodeA: Local write complete
    NodeB-->>NodeA: Peer B replication complete
    NodeC-->>NodeA: Peer C replication complete
    NodeA-->>Client: 200 OK (Return SHA-1 Key)
```

### Remote File Retrieval & Query Routing
When a client requests a file from Node A (`Get(key)`), Node A checks its local disk first. If the file is not found, Node A initiates a network search:

```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant NodeA as Node A
    participant LocalStore as Node A CAS
    participant PeerB as Peer B
    participant PeerC as Peer C

    Client->>NodeA: GET /files/:key
    NodeA->>LocalStore: Has(key)?
    
    alt Cache Hit (File exists locally)
        LocalStore-->>NodeA: File exists
        NodeA->>LocalStore: Read(key)
        LocalStore-->>NodeA: io.ReadCloser stream
        NodeA-->>Client: Stream file payload
    else Cache Miss (File missing locally)
        LocalStore-->>NodeA: File absent
        NodeA->>PeerB: Broadcast MessageGetFile(key)
        NodeA->>PeerC: Broadcast MessageGetFile(key)
        
        alt Peer B responds first with Stream
            PeerB-->>NodeA: Stream file payload over TCP
            par Cache locally
                NodeA->>LocalStore: Write(key, stream)
            and Stream to client
                NodeA-->>Client: Stream payload directly to HTTP response
            end
        else Timeout (No peer holds the file)
            NodeA-->>Client: 404 Not Found (ErrFileNotFound)
        end
    end
```

### Cluster Topology & Bootstrap Discovery
Nodes form an interconnected mesh by connecting to designated bootstrap nodes on startup:

```mermaid
graph TD
    classDef bootstrap fill:#3b82f6,stroke:#1d4ed8,stroke-width:2px,color:#fff;
    classDef worker fill:#10b981,stroke:#047857,stroke-width:2px,color:#fff;

    Node1["Node 1 (:3000)<br/>[Bootstrap Seed]"]:::bootstrap
    Node2["Node 2 (:4000)<br/>[Storage Peer]"]:::worker
    Node3["Node 3 (:5000)<br/>[Storage Peer]"]:::worker
    Node4["Node 4 (:6000)<br/>[Storage Peer]"]:::worker

    Node2 -->|Dial Bootstrap| Node1
    Node3 -->|Dial Bootstrap| Node1
    Node4 -->|Dial Bootstrap| Node2
    Node3 <-->|Peer Exchange| Node2
    Node4 <-->|Peer Exchange| Node3
```

---

## HTTP API & CLI Interface

Every node can optionally expose an HTTP server for client applications:

### Endpoints
| Method | Route | Description | Response |
| :--- | :--- | :--- | :--- |
| `POST` | `/files` | Upload file multipart form data | `{"key": "<sha1-hash>", "size": 1048576}` |
| `GET` | `/files/{key}` | Download file by its SHA-1 hash | Binary octet-stream with checksum header |
| `GET` | `/peers` | List currently connected peer addresses | `{"peers": [":4000", ":5000"]}` |

### Example CLI / cURL Usage
```bash
# Upload a document
curl -F "file=@presentation.pdf" http://localhost:8080/files
# Response: {"key":"a8f5c2d3e91b...","size":4582912}

# Download by hash
curl http://localhost:8080/files/a8f5c2d3e91b... --output downloaded.pdf

# Inspect cluster health
curl http://localhost:8080/peers
```

---

## Project Layout

```text
peerfs/
├── cmd/
│   ├── peerfs/               # Main PeerFs node daemon entry point
│   │   └── main.go
│   └── peerfs-cli/           # Optional command-line administrative client
│       └── main.go
├── p2p/                      # Peer-to-peer networking layer
│   ├── transport.go          # Transport & Peer interfaces
│   ├── tcp_transport.go      # TCP listener, dials, connection pooling
│   ├── handshake.go          # Peer handshake verification
│   ├── encoding.go           # Frame decoders & wire serializer
│   └── message.go            # RPC data structures
├── storage/                  # Content-Addressed Storage engine
│   ├── store.go              # Store implementation (Write, Read, Has, Delete)
│   ├── path.go               # Deterministic SHA-1 path sharding
│   └── store_test.go         # CAS unit test suite
├── server.go                 # FileServer orchestration & peer registry
├── server_test.go            # Multi-node integration test suite
├── go.mod                    # Module definition (peerfs)
└── README.md                 # System architecture & documentation
```

---

## Getting Started & Development

### Prerequisites
- **Go**: Version `1.22+` (or latest stable)

### Build
Compile the node daemon:
```powershell
go build ./cmd/peerfs/
```

### Run Node
```powershell
go run ./cmd/peerfs/
```

### Run Tests with Race Detection
Distributed systems with concurrent channels and network goroutines must be strictly verified for data races:
```powershell
go test -v -race ./...
```

---

## License
MIT License.
