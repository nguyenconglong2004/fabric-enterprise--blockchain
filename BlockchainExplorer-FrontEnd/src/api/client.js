// Simple API client for CoreService.
// Uses Vite dev proxy (/api -> http://localhost:8080) so we can call relative URLs.

export async function apiGet(path) {
  const res = await fetch(path, {
    method: 'GET',
    headers: {
      Accept: 'application/json',
    },
  });

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`GET ${path} failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ''}`);
  }

  // Some endpoints may return raw values (like /api/state). Try json first.
  const contentType = res.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return res.json();
  }
  return res.text();
}

export async function apiPostJson(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify(body),
  });

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`POST ${path} failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ''}`);
  }

  return res.json();
}

export async function getContracts() {
  return apiGet('/api/contracts');
}

export async function getCommittedBlocks(limit = 20) {
  return apiGet(`/api/blocks?limit=${encodeURIComponent(limit)}`);
}

export async function getCommittedTransactions(limit = 50) {
  return apiGet(`/api/transactions?limit=${encodeURIComponent(limit)}`);
}

export async function getBlockByHash(hash) {
  return apiGet(`/api/block?hash=${encodeURIComponent(hash)}`);
}

export async function submitTx(tx) {
  return apiPostJson('/api/tx/submit', tx);
}

export function createExplorerEventSource() {
  return new EventSource('/api/explorer/stream');
}
