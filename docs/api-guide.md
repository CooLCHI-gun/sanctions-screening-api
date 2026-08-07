# API Guide

Sanctions Screening API — full endpoint reference. Subscribe on
[RapidAPI](https://rapidapi.com/CooLCHI-gun/api/sanctions-screening-api) to get
your keys.

## Authentication

Two keys are required:

| Header | Description |
|--------|-------------|
| `X-RapidAPI-Key` | Your RapidAPI subscription key |
| `X-RapidAPI-Host` | `sanctions-screening-api2.p.rapidapi.com` |
| `X-API-Key` | Your tenant key (issued at onboarding) |

## POST /screen

Screen a name against all sanctions lists + your custom watchlists.

### Request

```json
{
  "query_name": "Kim Jong Un",
  "entity_type": "individual",
  "threshold": 0.7
}
```

| Field | Type | Description |
|-------|------|-------------|
| `query_name` | string | Name to screen (required) |
| `entity_type` | string | `individual` \| `organization` \| `vessel` \| `aircraft` |
| `threshold` | number | Fuzzy match threshold 0.0–1.0 (default 0.7) |

### Response

```json
{
  "request_id": "scr-1786071285238352260",
  "screened_at": "2026-08-07T02:54:45.238357168Z",
  "status": "pending",
  "total_matches": 1,
  "data_version": "2026-08-07",
  "sources_covered": ["OFAC-SDN", "EU-Consolidated", "UN-Consolidated"],
  "candidates": [
    {
      "entity_name": "Jong Un KIM",
      "entity_type": "individual",
      "list_name": "OFAC-SDN",
      "confidence_score": 1.0,
      "matched_fields": ["name"],
      "sanction_basis": "sanctions"
    }
  ]
}
```

### Status semantics

| Status | Meaning |
|--------|---------|
| `pass` | No match above threshold |
| `hit` | Match >= threshold, below 0.9 |
| `pending` | High-confidence sanctions match (>= 0.9) — requires human review |

## POST /watchlists/{name}

Upload a CSV watchlist (new version).

```
entity_id,name,type,country,dob,identifiers,tags
WL-001,ZHANG WEI,individual,CN,1985-01-01,ID123,high-risk
```

Uploads are versioned — each upload deactivates the previous version.

## GET /watchlists

List your watchlists + versions.

## GET /cases?status=pending

Review queue — high-confidence hits that need human decision.

## POST /cases/{id}

Decide a case: `approved`, `rejected`, `false_positive`.

## GET /audit

Query your append-only audit trail (all screening events).

## Data freshness

- Lists re-fetched **daily 03:30 UTC** from official sources.
- `data_version` + `sources_covered` in every response.
- Sources: OFAC SDN, EU Consolidated FSF, UN Security Council Consolidated.
