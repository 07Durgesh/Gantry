# ADR-001: Core Language Choice for Gantry (Go vs TypeScript/Node.js)

* **Status:** Accepted (Recommended: Go)
* **Date:** 2026-11-18
* **Deciders:** Senior Tech Lead & Apprentice Systems Engineer
* **Scope:** Core runtime engine, API server, queue workers, and CLI.

---

## 1. Context & Problem Statement

Gantry is an internal container deployment engine and dynamic ingress platform. Its core responsibilities include:
1. Direct communication with the Docker Engine daemon over Unix domain sockets (`/var/run/docker.sock`) and Windows named pipes (`//./pipe/dockerDesktopLinuxEngine`).
2. High-throughput, low-latency streaming of tarballs (build contexts) and multiplexed container stdout/stderr log streams.
3. Concurrency management for parallel container builds, health-checking loops, and proxy route registrations.
4. Serving as the primary portfolio and technical differentiator for an aspiring Systems/Platform Engineer targeting ₹12–45+ LPA and global remote infrastructure roles.

We need to finalize the programming language for the engine core, evaluating **Go** versus **TypeScript / Node.js**.

---

## 2. Decision Drivers

* **Systems Relevance & Resume Punch:** Modern cloud-native infrastructure (Docker/Moby, Kubernetes, Caddy, Terraform, Prometheus, Etcd) is almost exclusively written in Go. Demonstrating idiomatic Go signals immediate readiness for platform and infrastructure engineering teams.
* **First-Class Docker Engine Integration:** The official Docker client SDK (`github.com/docker/docker/client`) is native Go. It provides zero-overhead access to Docker API structs, tar stream manipulation, and multiplexed stream demuxing (`stdcopy.StdCopy`).
* **Concurrency Model:** Go's goroutines, channels, and `context.Context` provide a robust, readable, and resilient model for managing concurrent worker pools, timeouts, and cancellations without the callback/promise chain complexities of Node.js.
* **Binary Distribution:** Compiling into a single, self-contained static binary makes local CLI and server deployment seamless without dependency managers, node_modules bloat, or OS-level script execution policy quirks.
* **Learning Curve & Interview Defense:** Go forces explicit error handling and deep understanding of memory, IO streaming (`io.Reader` / `io.Writer`), and operating system interfaces—all prime targets for technical interview grilling.

---

## 3. Options Considered

### Option A: Go (Golang)
* **Pros:**
  * Native cloud-native lingua franca; direct alignment with Docker, Kubernetes, and Caddy.
  * Official Moby / Docker Go SDK maintained by Docker core engineers.
  * Standard library excellence: `net/http`, `archive/tar`, `io`, and `sync` allow building streaming pipelines with zero third-party dependencies.
  * Single static binary output (`gantry` and `gantryd`).
  * Outstanding resume impact for systems and backend infrastructure roles.
* **Cons:**
  * More verbose than JavaScript/TypeScript (no syntactic sugar like async/await).
  * BullMQ is TypeScript-native; in Go, Redis queues are implemented either via Redis Streams/Lists or libraries like `asynq`. (Note: Implementing queue logic with Redis Streams or `asynq` is actually an interview advantage).

### Option B: TypeScript / Node.js
* **Pros:**
  * Rich ecosystem for rapid web prototypes; BullMQ is the gold standard for Redis-based task queues.
  * Apprentice already has familiarity with JavaScript/TypeScript and backend web APIs.
* **Cons:**
  * Docker integration relies on third-party wrappers like `dockerode`, which wrap the Docker HTTP API rather than being the canonical source SDK.
  * Streaming binary tarballs and demultiplexing Docker raw stdio streams in Node streams is often clunky and prone to buffer memory leaks if backpressure is mishandled.
  * Resume signal: A TypeScript backend is often perceived by recruiters as "just another MERN/Node web developer" unless heavily defended, whereas Go immediately tags the candidate as a systems/backend engineer.
  * Windows PowerShell environment restrictions (e.g., script execution policies blocking `npm.ps1`).

---

## 4. Decision Outcome

**Chosen Option: Go (Golang 1.26+)**

### Rationale:
1. **Strategic Career Positioning:** Go immediately places the candidate in the top tier of freshers applying for backend and DevOps/Platform roles. Interviewers looking for infrastructure talent prioritize candidates who can talk fluently about goroutines, channels, and standard library IO streaming.
2. **Technical Alignment:** Gantry's heart is the Docker Engine API. Using Docker's official Go SDK (`github.com/docker/docker`) avoids abstraction layers and gives us direct control over the wire protocol.
3. **Environment:** Go 1.26 is already installed and verified on the local host.

---

## 5. Architectural Implications

* Project will be structured using standard Go package architecture: `cmd/` for entrypoints, `internal/` for private business logic, and `pkg/` for reusable components.
* Build queues will utilize Redis Streams or `hibiken/asynq`, exposing the apprentice to production-grade distributed queue concepts.
* Streaming log pipelines will use standard Go `io.Pipe()`, `io.MultiWriter()`, and `net/http` / WebSocket handlers.
