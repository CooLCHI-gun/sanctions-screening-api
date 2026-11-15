# MCP client setup

Remote server (Streamable HTTP), no API key needed for discovery or the free quota:

```json
{
  "mcpServers": {
    "sanctions": { "url": "https://sanc.mooocopy.com/mcp" }
  }
}
```

## Verify from a shell

```bash
curl -s https://sanc.mooocopy.com/.well-known/mcp.json

curl -s -X POST https://sanc.mooocopy.com/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

## Payment

Free tools have a small per-client daily quota. Priced calls settle per call in USDC on Base via
x402; a paid call without payment returns an `isError` tool result carrying the payment requirements.
