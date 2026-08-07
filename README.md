# Sanctions Screening API

Real-time sanctions screening against **OFAC SDN, EU Consolidated, and UN Security Council** lists — 26,000+ records, updated daily. Built for fintech, payments, and compliance teams.

**▶ Try it live on RapidAPI:** [rapidapi.com/CooLCHI-gun/api/sanctions-screening-api2](https://rapidapi.com/CooLCHIgun/api/sanctions-screening-api2)
**▶ Live health check:** [https://sanc.mooocopy.com/ready](https://sanc.mooocopy.com/ready)

> This repository shows the **public API surface and architecture** of the
> Sanctions Screening service. The production service is available via
> RapidAPI — subscribe there to use the full engine (5-tier matching, LLM
> review, multi-tenant audit trail). The core matching engine, data pipeline,
> and provider implementations are proprietary and not included here.

---

## Features

- **3 global lists, 1 call** — OFAC SDN, EU Consolidated, UN Security Council. No multi-vendor integration.
- **Fresh, verifiable data** — every response carries `data_version` + `sources_covered`; lists re-fetched daily from official sources.
- **Audit trail** — append-only, queryable log of every screening (compliance-ready).
- **Custom watchlists** — upload per-tenant CSV lists, screened alongside global lists.
- **Review queue** — high-confidence sanctions hits (>= 0.9) auto-flag for human review.
- **Multi-tenant** — per-tenant API keys, rate limits, LLM cascade opt-in (privacy by default).

## Quickstart

```bash
# Screen a name against all lists
curl -X POST "https://sanctions-screening-api2.p.rapidapi.com/screen" \
  -H "Content-Type: application/json" \
  -H "X-RapidAPI-Key: <YOUR_RAPIDAPI_KEY>" \
  -H "X-RapidAPI-Host: sanctions-screening-api2.p.rapidapi.com" \
  -H "X-API-Key: <YOUR_TENANT_KEY>" \
  -d '{"query_name": "Kim Jong Un", "entity_type": "individual", "threshold": 0.7}'
```

Tenant keys are issued during RapidAPI onboarding. See [API guide](docs/api-guide.md) for the full endpoint reference.

## Architecture

```
                 ┌─────────────────────────────────────────────┐
                 │            RapidAPI Gateway                 │
                 │  (auth, billing, X-RapidAPI-Proxy-Secret)   │
                 └──────────────────┬──────────────────────────┘
                                    │ HTTPS
                 ┌──────────────────▼──────────────────────────┐
                 │           HTTP API layer (this repo)        │
                 │  /screen  /watchlists  /cases  /audit       │
                 │  middleware: proxy-secret, admin-key,       │
                 │  rate-limit, body-limit, sanitizers         │
                 └──────┬──────────────┬──────────────┬────────┘
                        │              │              │
        ┌───────────────▼───┐  ┌───────▼───────┐  ┌───▼────────────┐
        │  Matching engine  │  │  Tenant store │  │  Audit / Review│
        │  (proprietary)    │  │  (SQLite)     │  │  (SQLite)      │
        │  5-tier pipeline  │  │  keys, tiers  │  │  append-only   │
        └───────────────┬───┘  └───────────────┘  └────────────────┘
                        │
        ┌───────────────▼───────────────────────────────┐
        │  Data pipeline (proprietary)                  │
        │  OFAC + EU + UN → normalize → merge → deploy  │
        └───────────────────────────────────────────────┘
```

**This repo includes:** HTTP API handlers, middleware, tenant/auth, watchlist,
audit, review, models, config, bootstrap wiring — the request-handling layer.

**Proprietary (not included):** matching pipeline (4-tier fuzzy + LLM review),
provider implementations (OFAC/EU/UN fetch + normalize), data update scripts,
deployment tooling.

## Repository layout

```
internal/
├── httpapi/     # Handlers + middleware (proxy secret, admin key, rate limit)
├── tenant/      # Per-tenant API keys, tiers (SHA-256 hashed)
├── audit/       # Append-only SQLite audit trail (WAL)
├── watchlist/   # CSV upload + versioning
├── review/      # Case review queue (pending/approved/rejected)
├── models/      # Domain types
├── config/      # Env-based config
└── bootstrap/   # Startup wiring
openapi/         # OpenAPI 3.0.3 spec (also powers the RapidAPI listing)
docs/            # Architecture + API guide
examples/        # Client code samples
```

## Data freshness

- Lists re-fetched **daily (03:30 UTC)** from official sources.
- `data_version` in every response (e.g. `2026-08-07`).
- Sources: OFAC SDN (treasury.gov), EU Consolidated FSF, UN Security Council Consolidated List.

## License

**PolyForm Shield 1.0.0** — source-available. You may read, use, and modify
the code for non-competing purposes. You may **not** use it to offer a
competing sanctions-screening product or service. See [LICENSE](LICENSE).

For commercial licensing (full engine incl. matching + data pipeline),
contact via RapidAPI.

## Security

See [SECURITY.md](SECURITY.md) for responsible disclosure.
