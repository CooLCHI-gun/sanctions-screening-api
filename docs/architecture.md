# Architecture

High-level design of the Sanctions Screening service. The request-handling
layer shown here is open; the matching engine and data pipeline are
proprietary.

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

## Design decisions

- **Go stdlib net/http** — no frameworks; small dependency surface for a
  security-sensitive service.
- **SQLite (modernc.org/sqlite, pure Go)** — ACID, queryable, zero-ops.
  Four databases: tenants, audit, watchlists, review.
- **Append-only audit** — no update/delete API on audit records; compliance
  teams can trust the trail.
- **Proxy-secret validation** — origin rejects requests without the RapidAPI
  `X-RapidAPI-Proxy-Secret` header, so billing can't be bypassed by hitting
  origin directly.
- **Privacy by default** — LLM-assisted review is opt-in per tenant, sends
  minimal fields (name + entity type only), and is capped monthly.
- **Defense in depth** — admin key, body limits, CSV formula guard, header
  sanitization, prompt-injection hardening on any LLM path.

## Request flow (screening)

1. RapidAPI validates subscription + billing, forwards with proxy secret.
2. HTTP layer validates tenant key, rate limit, body size.
3. Screening service runs the matching pipeline against global lists +
   tenant watchlists.
4. High-confidence sanctions hits (>= 0.9) create a review case.
5. Every event appended to the audit trail.
6. Response includes `data_version` + `sources_covered` for freshness proof.
