/**
 * k6 — POST /api/tx/submit (Core :8080)
 *
 * Đo throughput thật (tx/s, block/s trên ledger): sau test chạy
 *   curl "http://localhost:8080/api/metrics/e2e?window=120&tx_prefix=k6-"
 * k6 chỉ tạo tải; RATE thấp sẽ GIỚI HẠN phía client — đừng dùng steady để tìm max hệ thống.
 *
 * Đẩy tối đa (mặc định) — mỗi VU submit liên tục, không cap req/s:
 *   k6 run submit-tx.js
 *   k6 run -e VUS=400 -e DURATION=90s submit-tx.js
 *
 * Open-loop — cố gắng bắn OPEN_RATE req/s (k6 tự tăng VU tới maxVUs):
 *   k6 run -e SCENARIO=open -e OPEN_RATE=5000 submit-tx.js
 *
 * Ramp — tăng dần để tìm điểm bão hòa:
 *   k6 run -e SCENARIO=ramp submit-tx.js
 *
 * Cố định tải (smoke / so sánh):
 *   k6 run -e SCENARIO=steady -e RATE=50 -e VUS=80 submit-tx.js
 */

import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const DURATION = __ENV.DURATION || '60s';
const VUS = Number(__ENV.VUS || 300);
const CONTRACT = __ENV.CONTRACT || 'example_asset';
const SCENARIO = (__ENV.SCENARIO || 'maxpush').toLowerCase();
const REQ_TIMEOUT = __ENV.REQ_TIMEOUT || '60s';

// steady / open
const RATE = Number(__ENV.RATE || 100);
const OPEN_RATE = Number(__ENV.OPEN_RATE || 5000);
const RAMP_PEAK = Number(__ENV.RAMP_PEAK || 3000);

const submitOk = new Counter('submit_ok');
const submitFail = new Counter('submit_fail');
const submitLatency = new Trend('submit_latency_ms', true);

const maxVUsOpen = Math.max(Number(__ENV.MAX_VUS || 600), VUS * 2);

function buildScenarios() {
  switch (SCENARIO) {
    case 'steady':
      return {
        steady: {
          executor: 'constant-arrival-rate',
          rate: RATE,
          timeUnit: '1s',
          duration: DURATION,
          preAllocatedVUs: Math.min(VUS, RATE * 2),
          maxVUs: Math.max(VUS * 2, RATE + 50),
        },
      };

    case 'open':
      // Open model: k6 cố giữ OPEN_RATE iter/s — phù hợp khi muốn “ép” hàng nghìn offer/s
      return {
        open: {
          executor: 'constant-arrival-rate',
          rate: OPEN_RATE,
          timeUnit: '1s',
          duration: DURATION,
          preAllocatedVUs: Math.min(maxVUsOpen, 400),
          maxVUs: maxVUsOpen,
        },
      };

    case 'ramp':
      return {
        ramp: {
          executor: 'ramping-arrival-rate',
          startRate: 50,
          timeUnit: '1s',
          preAllocatedVUs: 100,
          maxVUs: maxVUsOpen,
          stages: [
            { duration: '20s', target: 200 },
            { duration: '20s', target: 500 },
            { duration: '20s', target: 1000 },
            { duration: '20s', target: RAMP_PEAK },
            { duration: '30s', target: RAMP_PEAK },
            { duration: '20s', target: 500 },
            { duration: '10s', target: 100 },
          ],
        },
      };

    case 'burst':
      // Giữ tên cũ — ramp ngắn (legacy)
      return {
        burst: {
          executor: 'ramping-arrival-rate',
          startRate: 100,
          timeUnit: '1s',
          preAllocatedVUs: Math.max(VUS, 150),
          maxVUs: maxVUsOpen,
          stages: [
            { duration: '15s', target: 500 },
            { duration: '30s', target: 1500 },
            { duration: '30s', target: RAMP_PEAK },
            { duration: '15s', target: 500 },
          ],
        },
      };

    case 'maxpush':
    default:
      // Closed loop: N VU submit liên tục — throughput ≈ f(latency), không bị cap bởi RATE
      return {
        maxpush: {
          executor: 'constant-vus',
          vus: VUS,
          duration: DURATION,
        },
      };
  }
}

export const options = {
  scenarios: buildScenarios(),
  thresholds: {
    submit_ok: ['count>0'],
    // Không siết fail/latency khi maxpush — hệ thống có thể nghẽn; xem /api/metrics/e2e
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
    submitted_at_ms: Date.now(),
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

function scenarioLabel() {
  switch (SCENARIO) {
    case 'steady':
      return `steady cap ${RATE} req/s, VUS≤${Math.max(VUS * 2, RATE + 50)}, ${DURATION}`;
    case 'open':
      return `open-loop target ${OPEN_RATE} req/s, maxVUs=${maxVUsOpen}, ${DURATION}`;
    case 'ramp':
      return `ramp → peak ${RAMP_PEAK} req/s, ${DURATION} + stages`;
    case 'burst':
      return `burst ramp → ${RAMP_PEAK} req/s`;
    default:
      return `maxpush ${VUS} VUs × loop (no RATE cap), ${DURATION}`;
  }
}

export function setup() {
  const res = http.get(`${BASE_URL}/api/contracts`, { timeout: '5s' });
  if (res.status !== 200) {
    throw new Error(
      `Core API không phản hồi tại ${BASE_URL} (status ${res.status}).`,
    );
  }
  console.log(`setup ok: ${BASE_URL}`);
  console.log(`scenario: ${scenarioLabel()}`);
  console.log(
    'Sau test: curl -s "http://localhost:8080/api/metrics/e2e?window=120&tx_prefix=k6-"',
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

  if (ok) {
    submitOk.add(1);
  } else {
    submitFail.add(1);
    if (__ITER < 3) {
      console.warn(`fail vu=${__VU} status=${res.status} body=${res.body}`);
    }
  }
}

export function teardown(data) {
  console.log(`done: ${data.baseUrl}`);
  console.log(
    'Ledger throughput (full flow): GET /api/metrics/e2e?window=<test_seconds>&tx_prefix=k6-',
  );
}
