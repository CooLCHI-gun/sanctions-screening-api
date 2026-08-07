// Sanctions Screening API — JavaScript client example
// Subscribe on RapidAPI: https://rapidapi.com/CooLCHI-gun/api/sanctions-screening-api

const RAPIDAPI_KEY = process.env.RAPIDAPI_KEY;
const TENANT_KEY = process.env.SANCTIONS_TENANT_KEY;
const HOST = "sanctions-screening-api2.p.rapidapi.com";

async function screen(queryName, entityType = "individual", threshold = 0.7) {
  const resp = await fetch(`https://${HOST}/screen`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-RapidAPI-Key": RAPIDAPI_KEY,
      "X-RapidAPI-Host": HOST,
      "X-API-Key": TENANT_KEY,
    },
    body: JSON.stringify({ query_name: queryName, entity_type: entityType, threshold }),
  });
  if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${await resp.text()}`);
  return resp.json();
}

const result = await screen("Kim Jong Un");
console.log(`status=${result.status} matches=${result.total_matches} version=${result.data_version}`);
for (const c of result.candidates) {
  console.log(`  - ${c.entity_name} conf=${c.confidence_score.toFixed(2)} list=${c.list_name}`);
}
