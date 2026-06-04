# Architecture — package dependency graph

Arrows mean **"imports / depends on"**. Every edge points inward toward `internal/domain`, which depends on nothing internal. Regenerate by re-auditing imports (`grep -rhoE '"github.com/dali/go_clean_arch_sample/[^"]+"' <pkg>`).

```mermaid
graph TD
    %% ---------- entrypoints ----------
    subgraph CMD["cmd · entrypoints (composition roots)"]
        APIMAIN["cmd/api"]
        CLI["cmd/cli"]
    end

    %% ---------- http delivery adapter ----------
    subgraph HTTP["internal/adapter/http · delivery"]
        APIPKG["api"]
        ROUTER["router"]
        HANDLER["handler"]
        MW["middleware"]
        HTTPERR["httperr"]
    end

    %% ---------- repository adapter ----------
    subgraph REPO["internal/adapter/repository"]
        PG["postgres"]
    end

    %% ---------- core ----------
    SVC["internal/usecase"]
    DOM["internal/domain"]

    %% ---------- cross-cutting ----------
    subgraph XC["cross-cutting"]
        CFG["internal/config"]
        LOG["internal/log"]
    end

    %% ---------- external infra (confined) ----------
    GORM[("gorm.io · pgconn")]

    %% entrypoint edges
    APIMAIN --> APIPKG
    APIMAIN --> PG
    APIMAIN --> CFG
    APIMAIN --> LOG
    APIMAIN -. "*gorm.DB handle" .-> GORM
    CLI --> PG
    CLI --> CFG
    CLI --> LOG

    %% http internal edges
    APIPKG --> ROUTER
    APIPKG --> MW
    APIPKG --> SVC
    ROUTER --> HANDLER
    ROUTER --> SVC
    HANDLER --> SVC
    HANDLER --> DOM
    HANDLER --> HTTPERR
    HANDLER --> LOG
    MW --> HTTPERR
    MW --> LOG

    %% repository edges
    PG --> SVC
    PG --> DOM
    PG --> CFG
    PG --> LOG
    PG --> GORM

    %% core + cross-cutting
    SVC --> DOM
    CFG --> LOG

    %% styling
    classDef core fill:#2e7d32,color:#fff,stroke:#1b5e20;
    classDef infra fill:#b71c1c,color:#fff,stroke:#7f0000;
    classDef xcut fill:#455a64,color:#fff,stroke:#263238;
    class DOM,SVC core;
    class GORM infra;
    class CFG,LOG xcut;
```

## Reading the direction

- **`domain` is the sink** — no outgoing internal edges. `usecase` → `domain` only. Nothing points out of the core.
- **`gorm.io · pgconn` (red) is reachable from exactly two places**: `internal/adapter/repository/postgres` (which owns it) and `cmd/api` (which only holds the `*gorm.DB` handle to open/close it — the dashed edge). The entire `http` subgraph has no path to it.
- **The `http` delivery layer depends only on `usecase` + `domain`** (plus `httperr`/`log`). `api`/`router`/`handler` never reach `postgres` or `gorm` — this is the swap-resistance the `usecase.Repositories` bundle buys: replacing the ORM is confined to `postgres/` + one type in `cmd/api`.
- **Cross-cutting `log`/`config` (grey)** are leaf utilities anything may use; `log` depends on nothing internal, `config` only on `log`.
- **`cmd/api` is the single node touching both the delivery adapter and the repository adapter** — the textbook composition-root shape.
