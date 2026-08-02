// Simple API client for CoreService.
// Uses Vite dev proxy (/api -> http://localhost:8080) so we can call relative URLs.

function authHeaders(token) {
  if (!token) return {};
  return {
    Authorization: `Bearer ${token}`,
    'X-Auth-Token': token,
  };
}

export async function apiGet(path, { token } = {}) {
  const res = await fetch(path, {
    method: 'GET',
    headers: {
      Accept: 'application/json',
      ...authHeaders(token),
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

export async function apiPostJson(path, body, { token } = {}) {
  const res = await fetch(path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...authHeaders(token),
    },
    body: JSON.stringify(body),
  });

  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`POST ${path} failed: ${res.status} ${res.statusText}${text ? ` - ${text}` : ''}`);
  }

  return res.json();
}

export async function login(username, password) {
  return apiPostJson('/api/auth/login', { username, password });
}

export async function logout(token) {
  return apiPostJson('/api/auth/logout', {}, { token });
}

export async function getMe(token) {
  const qs = token ? `?token=${encodeURIComponent(token)}` : '';
  return apiGet(`/api/me${qs}`, { token });
}

export async function getWalletBalance(tokenOrAddress) {
  if (tokenOrAddress && tokenOrAddress.length === 40 && /^[0-9a-fA-F]+$/.test(tokenOrAddress)) {
    return apiGet(`/api/wallet/balance?address=${encodeURIComponent(tokenOrAddress)}`);
  }
  return apiGet('/api/wallet/balance', { token: tokenOrAddress });
}

export async function getContracts() {
  return apiGet('/api/contracts');
}

export async function getCommittedBlocks(limit = 20) {
  return apiGet(`/api/blocks?limit=${encodeURIComponent(limit)}`);
}

export async function getCommittedTransactions(limit = 50, { username, token } = {}) {
  const qs = new URLSearchParams({ limit: String(limit) });
  if (username) qs.set('username', username);
  return apiGet(`/api/transactions?${qs.toString()}`, { token });
}

export async function getBlockByHash(hash) {
  return apiGet(`/api/block?hash=${encodeURIComponent(hash)}`);
}

export async function submitTx(tx) {
  return apiPostJson('/api/tx/submit', tx);
}

export async function submitTxAuthed(tx, token) {
  // Also pass ?token= — same pattern as balance SSE; some proxies mangle Authorization on POST.
  const qs = token ? `?token=${encodeURIComponent(token)}` : '';
  return apiPostJson(`/api/tx/submit${qs}`, tx, { token });
}

export function createExplorerEventSource() {
  return new EventSource('/api/explorer/stream');
}

/** EventSource cannot set Authorization — pass token + pinned address as query. */
export function createBalanceEventSource(token, address) {
  const qs = new URLSearchParams();
  if (token) qs.set('token', token);
  if (address) qs.set('address', String(address).trim().toLowerCase());
  return new EventSource(`/api/wallet/balance/stream?${qs.toString()}`);
}
