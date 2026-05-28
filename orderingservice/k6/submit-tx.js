/**
 * k6 — POST /api/tx/submit (Core :8080)
 *
 * Mặc định: open-loop đều — RATE req/s cố định (không phụ thuộc latency).
 *
 *   k6 run submit-tx.js
 *   k6 run -e RATE=1200 -e DURATION=10s submit-tx.js
 *   k6 run -e SCENARIO=maxpush -e VUS=200 -e DURATION=5s submit-tx.js
 *
 * Sau test: GET /api/metrics/throughput?window=1&tx_prefix=k6-
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const DURATION = __ENV.DURATION || '10s';
const CONTRACT = __ENV.CONTRACT || 'example_asset';
const TX_PREFIX = __ENV.TX_PREFIX || 'k6-';
const REQ_TIMEOUT = __ENV.REQ_TIMEOUT || '30s';
const LEDGER_WAIT = __ENV.LEDGER_WAIT || '8s';

// steady (mặc định): số request mỗi giây (open-loop, đều theo thời gian)
const RATE = Number(__ENV.RATE || 2000);
// max VU k6 được phép mở để đạt RATE (nên ≥ RATE khi latency > ~1s)
const MAX_VUS = Number(__ENV.MAX_VUS || Math.max(400, RATE + 100));
const PRE_VUS = Number(__ENV.PRE_VUS || Math.min(MAX_VUS, Math.max(RATE, 50)));

// maxpush: N VU loop nhanh nhất có thể (burst, không đều)
const VUS = Number(__ENV.VUS || 100);
const SCENARIO = (__ENV.SCENARIO || 'steady').toLowerCase();

const submitOk = new Counter('submit_ok');
const submitFail = new Counter('submit_fail');
const submitLatency = new Trend('submit_latency_ms', true);

function buildScenarios() {
  switch (SCENARIO) {
    case 'maxpush':
    case 'burst':
      return {
        load: {
          executor: 'constant-vus',
          vus: VUS,
          duration: DURATION,
        },
      };
    case 'steady':
    default:
      return {
        load: {
          executor: 'constant-arrival-rate',
          rate: RATE,
          timeUnit: '1s',
          duration: DURATION,
          preAllocatedVUs: PRE_VUS,
          maxVUs: MAX_VUS,
        },
      };
  }
}

export const options = {
  scenarios: buildScenarios(),
  thresholds: {
    submit_ok: ['count>0'],
  },
};

function jsonToPayloadHex(obj) {
  const s = JSON.stringify(obj);
  const bytes = [];
  for (let i = 0; i < s.length; i++) {
    bytes.push(s.charCodeAt(i).toString(16).padStart(2, '0'));
  }
  return bytes.join('');
}

function buildTx(vu, iter) {
  const id = `${TX_PREFIX}${vu}-${iter}-${Date.now()}`;
  return {
    txid: id,
    version: 1,
    locktime: 0,
    signature: '',
    client_pubkey: '',
    sender_pubkey: '',
    vin: [],
    vout: [],
    contract_name: CONTRACT,
    function_name: 'execute',
    payload: jsonToPayloadHex({
      id,
      color: 'blue',
      action: 'create',
    }),
  };
}

function isSuccess(body) {
  try {
    const j = JSON.parse(body);
    return j.status === 'success' && j.tx_id;
  } catch {
    return false;
  }
}

function parseDurationSec(s) {
  const m = String(s).match(/^(\d+(?:\.\d+)?)(ms|s|m|h)?$/);
  if (!m) return 10;
  const n = parseFloat(m[1]);
  switch (m[2] || 's') {
    case 'ms':
      return n / 1000;
    case 'm':
      return n * 60;
    case 'h':
      return n * 3600;
    default:
      return n;
  }
}

function scenarioLabel() {
  const sec = parseDurationSec(DURATION);
  if (SCENARIO === 'maxpush' || SCENARIO === 'burst') {
    return `maxpush ${VUS} VUs × ${DURATION} (closed-loop, không đều)`;
  }
  const approx = Math.round(RATE * sec);
  return `steady ${RATE} req/s × ${DURATION} ≈ ${approx} tx (open-loop, đều)`;
}

export function setup() {
  const res = http.get(`${BASE_URL}/api/contracts`, { timeout: '5s' });
  if (res.status !== 200) {
    throw new Error(`Core API không phản hồi tại ${BASE_URL} (status ${res.status})`);
  }
  console.log(`→ ${BASE_URL}`);
  console.log(`scenario: ${scenarioLabel()}`);
  console.log(
    'metrics: curl -s "' +
      BASE_URL +
      '/api/metrics/throughput?window=1&tx_prefix=' +
      TX_PREFIX +
      '"',
  );
  return { baseUrl: BASE_URL };
}

export default function () {
  const tx = buildTx(__VU, __ITER);
  const res = http.post(`${BASE_URL}/api/tx/submit`, JSON.stringify(tx), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'tx-submit' },
    timeout: REQ_TIMEOUT,
  });

  submitLatency.add(res.timings.duration);

  const ok =
    res.status === 200 &&
    check(res, {
      'status 200': (r) => r.status === 200,
      'core accepted': (r) => isSuccess(r.body),
    });

  if (ok) submitOk.add(1);
  else submitFail.add(1);
}

export function teardown(data) {
  const waitSec = parseDurationSec(LEDGER_WAIT);
  if (waitSec > 0) {
    console.log(`chờ ledger ${LEDGER_WAIT}...`);
    sleep(waitSec);
  }

  const prefix = encodeURIComponent(TX_PREFIX);
  const url = `${data.baseUrl}/api/metrics/throughput?window=1&tx_prefix=${prefix}`;
  const res = http.get(url, { timeout: '15s' });

  if (res.status !== 200) {
    console.error(`throughput API failed: ${res.status} ${res.body}`);
    return;
  }

  let m;
  try {
    m = JSON.parse(res.body);
  } catch {
    console.error('throughput parse error:', res.body);
    return;
  }

  if (m.status !== 'success') {
    console.error('throughput:', res.body);
    return;
  }

  console.log('--- ledger throughput (1s @ latest commit) ---');
  console.log(`tx_committed: ${m.tx_committed}`);
  console.log(`tx_per_sec:     ${m.tx_per_sec}`);
  console.log(`blocks:         ${m.blocks_committed} (${m.blocks_per_sec}/s)`);
  console.log(`window:         ${m.window_seconds}s (${m.window_start || '?'} → ${m.window_end || '?'})`);
  console.log(`query:          ${url}`);
}
