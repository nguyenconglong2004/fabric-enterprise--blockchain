/**
 * k6 — POST /api/tx/submit (Core :8080, cùng FE)
 *
 * Chạy nhanh (mặc định ~50 req/s):
 *   k6 run submit-tx.js
 *   k6 run -e RATE=100 -e VUS=80 submit-tx.js
 *
 * Burst (gom nhiều tx vào pool orderer trong 200ms):
 *   k6 run -e SCENARIO=burst submit-tx.js
 */

import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const RATE = Number(__ENV.RATE || 50);
const DURATION = __ENV.DURATION || '60s';
const VUS = Number(__ENV.VUS || 50);
const CONTRACT = __ENV.CONTRACT || 'example_asset';
const SCENARIO = (__ENV.SCENARIO || 'steady').toLowerCase();

const submitOk = new Counter('submit_ok');
const submitFail = new Counter('submit_fail');
const submitLatency = new Trend('submit_latency_ms', true);

const scenarios =
  SCENARIO === 'burst'
    ? {
        burst: {
          executor: 'ramping-arrival-rate',
          startRate: 10,
          timeUnit: '1s',
          preAllocatedVUs: Math.max(VUS, 80),
          maxVUs: Math.max(VUS * 2, 150),
          stages: [
            { duration: '10s', target: 20 },
            { duration: '20s', target: 20 },
            { duration: '20s', target: 20 },
            { duration: '10s', target: 20 },
          ],
        },
      }
    : {
        steady: {
          executor: 'constant-arrival-rate',
          rate: RATE,
          timeUnit: '1s',
          duration: DURATION,
          preAllocatedVUs: VUS,
          maxVUs: Math.max(VUS * 2, 100),
        },
      };

export const options = {
  scenarios,
  thresholds: {
    http_req_failed: ['rate<0.25'],
    submit_ok: ['count>0'],
  },
};

function jsonToPayloadHex(obj) {
  const s = JSON.stringify(obj);
  let hex = '';
  for (let i = 0; i < s.length; i++) {
    hex += s.charCodeAt(i).toString(16).padStart(2, '0');
  }
  return hex;
}

function buildTx(vu, iter) {
  const id = `k6-${vu}-${iter}-${Date.now()}`;
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

export function setup() {
  const res = http.get(`${BASE_URL}/api/contracts`, { timeout: '5s' });
  if (res.status !== 200) {
    throw new Error(
      `Core API không phản hồi tại ${BASE_URL} (status ${res.status}).`,
    );
  }
  console.log(`setup ok: ${BASE_URL} scenario=${SCENARIO}`);
  return { baseUrl: BASE_URL };
}

export default function () {
  const tx = buildTx(__VU, __ITER);
  const res = http.post(`${BASE_URL}/api/tx/submit`, JSON.stringify(tx), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: 'tx-submit' },
    timeout: '60s',
  });

  submitLatency.add(res.timings.duration);

  const ok =
    res.status === 200 &&
    check(res, {
      'status 200': (r) => r.status === 200,
      'core accepted': (r) => isSuccess(r.body),
    });

  if (ok) {
    submitOk.add(1);
  } else {
    submitFail.add(1);
    if (__ITER < 5) {
      console.warn(`fail vu=${__VU} status=${res.status} body=${res.body}`);
    }
  }
}

export function teardown(data) {
  console.log(`done: ${data.baseUrl}`);
}
