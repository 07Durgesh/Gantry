# 🏗️ Gantry

> **The container deployment & dynamic ingress engine.**  
> Inspired by the industrial gantry cranes that hoist and schedule freight containers in shipping ports.

Gantry is a lightweight developer platform (Mini-PaaS) written in **Go**. It interfaces directly with the **Docker Engine API**, orchestrates background build pipelines, enforces cgroup resource constraints, and registers subdomains on a **Caddy** reverse proxy with zero downtime.

---

## 🎯 Architecture

```text
               ┌───────────────────────────────┐
               │          Gantry CLI           │
               └───────────────┬───────────────┘
                               │ 1. gantry deploy
                               ▼
               ┌───────────────────────────────┐
               │         Gantry Server         │
               │   (API, Orchestrator, PortMgr)│
               └───────┬───────────────┬───────┘
                       │               │
     2. Enqueue Job    │               │ 4. Register Route via HTTP API
                       ▼               ▼
        ┌──────────────────┐       ┌───────────────────────────────┐
        │  Redis / Asynq   │       │ Reverse Proxy (Caddy API)     │
        │  (Build Queue)   │       │ (Auto-SSL, Dynamic Subdomains)│
        └────────┬─────────┘       └───────────────┬───────────────┘
                 │                                 │
    3. Pull Job  │                                 │ 5. Proxy Traffic
                 ▼                                 │    app-1.localhost
        ┌──────────────────┐                       │
        │   Build Worker   │                       │
        └────────┬─────────┘                       │
                 │ 3a. Docker Socket / Named Pipe  ▼
                 ▼                         ┌───────────────┐
        ┌──────────────────┐               │  Deployed     │
        │  Docker Daemon   ├──────────────►│  User App     │
        │  (cgroups/ports) │               │  Container    │
        └──────────────────┘               └───────────────┘
```

---

## 📂 Project Structure

```
.
├── cmd/
│   ├── gantry/             # Gantry CLI client
│   └── gantryd/            # Gantry API server & orchestrator daemon
├── internal/
│   ├── engine/             # Direct Docker Engine API client & lifecycle manager
│   ├── builder/            # Git cloning & tarball context streaming builder
│   ├── ingress/            # Caddy reverse proxy integration (Zero-downtime routing)
│   ├── queue/              # Asynchronous job queue (Redis)
│   ├── config/             # Configuration & environment management
│   └── models/             # Domain entities & deployment state machine
├── docs/
│   └── adr/                # Architecture Decision Records
│       └── 001-core-language-selection.md
├── examples/
│   └── sample-app/         # Reference application used to test the build engine
├── go.mod
└── README.md
```

---

## 🧭 Milestone Roadmap

- [ ] **Milestone 1: Container Runtime Engine**
  - Direct Docker Engine API communication via socket / Windows named pipe.
  - In-memory tarball streaming and image building (`POST /build`).
  - Dynamic host port allocation and container lifecycle management (`start`, `stop`, `inspect`, `remove`).
- [ ] **Milestone 2: Asynchronous Build Pipelines & Log Streaming**
  - Redis worker queue decouples builds from the HTTP API.
  - Real-time stdout/stderr log multiplexing to client terminals.
- [ ] **Milestone 3: Dynamic Ingress, Zero-Downtime Routing & cgroups**
  - Dynamic route registration via Caddy REST API without proxy reload drops.
  - Health check polling (`/healthz`) before traffic switchover.
  - Hard memory and CPU limits (`HostConfig` cgroups).
- [ ] **Milestone 4: Developer CLI, Architecture Journal & Interview Defense**
  - Full CLI tooling (`gantry deploy`, `gantry logs`, `gantry ps`).
  - Live mock interview defense and technical trade-off breakdown.
