/**
 * k6 — POST /api/tx/submit (Core :8080)
 *
 *   k6 run submit-tx.js
 *   k6 run -e RATE=5000 -e DURATION=20s -e MAX_VUS=6000 submit-tx.js
 *   k6 run -e SCENARIO=sweep submit-tx.js
 *   k6 run -e CONTRACT=bench_ping -e SCENARIO=sweep submit-tx.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const DURATION = __ENV.DURATION || '25s';
const CONTRACT = __ENV.CONTRACT || 'bench_ping';
const TX_PREFIX = __ENV.TX_PREFIX || 'k6-';
const REQ_TIMEOUT = __ENV.REQ_TIMEOUT || '30s';
const LEDGER_WAIT = __ENV.LEDGER_WAIT || '15s';

const RATE = Number(__ENV.RATE || 6000);
const MAX_VUS = Number(__ENV.MAX_VUS || Math.max(800, RATE + 800));
const PRE_VUS = Number(__ENV.PRE_VUS || Math.min(MAX_VUS, Math.max(RATE, 100)));

const VUS = Number(__ENV.VUS || 300);
const SCENARIO = (__ENV.SCENARIO || 'steady').toLowerCase();

// sweep: tăng dần để tìm điểm bão hòa (ledger peak có tăng không)
const SWEEP_START = Number(__ENV.SWEEP_START || 4000);
const SWEEP_PEAK = Number(__ENV.SWEEP_PEAK || 10000);
const SWEEP_STEP = Number(__ENV.SWEEP_STEP || 1500);
const SWEEP_STAGE_SEC = Number(__ENV.SWEEP_STAGE_SEC || 15);

const submitOk = new Counter('submit_ok');
const submitFail = new Counter('submit_fail');
const submitLatency = new Trend('submit_latency_ms', true);

function sweepStages() {
  const stages = [];
  for (let target = SWEEP_START; target <= SWEEP_PEAK; target += SWEEP_STEP) {
    stages.push({ duration: `${SWEEP_STAGE_SEC}s`, target });
  }
  return stages;
}

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

    case 'sweep':
      return {
        sweep: {
          executor: 'ramping-arrival-rate',
          startRate: SWEEP_START,
          timeUnit: '1s',
          preAllocatedVUs: Math.min(800, SWEEP_START + 200),
          maxVUs: MAX_VUS,
          stages: sweepStages(),
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

function buildPayload(vu, iter) {
  const id = `${TX_PREFIX}${vu}-${iter}`;
  if (CONTRACT === 'bench_ping') {
    return { v: id };
  }
  return {
    id: `${id}-${Date.now()}`,
    color: 'blue',
    action: 'create',
  };
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
    payload: jsonToPayloadHex(buildPayload(vu, iter)),
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
  if (!m) return 20;
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
    return `maxpush ${VUS} VUs × ${DURATION}`;
  }
  if (SCENARIO === 'sweep') {
    return `sweep ${SWEEP_START}→${SWEEP_PEAK} req/s (+${SWEEP_STEP}/${SWEEP_STAGE_SEC}s)`;
  }
  return `steady ${RATE} req/s × ${DURATION} ≈ ${Math.round(RATE * sec)} submit`;
}

function fetchMetrics(baseUrl, query) {
  const res = http.get(`${baseUrl}/api/metrics/throughput?${query}`, { timeout: '15s' });
  if (res.status !== 200) {
    console.error(`metrics failed (${query}): ${res.status} ${res.body}`);
    return null;
  }
  try {
    const m = JSON.parse(res.body);
    return m.status === 'success' ? m : null;
  } catch {
    return null;
  }
}

function logMetrics(label, m) {
  if (!m) return;
  console.log(`--- ${label} ---`);
  console.log(`tx_committed: ${m.tx_committed}`);
  console.log(`tx_per_sec:     ${m.tx_per_sec}`);
  console.log(`blocks:         ${m.blocks_committed} (${m.blocks_per_sec}/s)`);
  if (m.window_start) {
    console.log(`window:         ${m.window_seconds}s (${m.window_start} → ${m.window_end})`);
  }
}

export function setup() {
  const res = http.get(`${BASE_URL}/api/contracts`, { timeout: '5s' });
  if (res.status !== 200) {
    throw new Error(`Core API không phản hồi tại ${BASE_URL} (status ${res.status})`);
  }
  console.log(`→ ${BASE_URL}`);
  console.log(`contract: ${CONTRACT}`);
  console.log(`scenario: ${scenarioLabel()}`);
  return { baseUrl: BASE_URL, loadStart: new Date().toISOString() };
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

function fetchBenchmark(baseUrl, since, until, prefix) {
  const q = [
    `since=${encodeURIComponent(since)}`,
    `until=${encodeURIComponent(until)}`,
    `tx_prefix=${prefix}`,
  ].join('&');
  const res = http.get(`${baseUrl}/api/metrics/benchmark?${q}`, { timeout: '15s' });
  if (res.status !== 200) {
    console.error(`benchmark failed: ${res.status} ${res.body}`);
    return null;
  }
  try {
    const m = JSON.parse(res.body);
    return m.status === 'success' ? m : null;
  } catch {
    return null;
  }
}

function logBenchmark(label, m, k6Stats) {
  if (!m) return;
  console.log(`--- ${label} ---`);
  console.log(`window: ${m.window_seconds}s (${m.window_start} → ${m.window_end})`);
  if (k6Stats) {
    console.log(`k6 submit_ok: ${k6Stats.ok}  fail: ${k6Stats.fail}  fail%: ${k6Stats.failPct}`);
    console.log(`k6 submit sustained: ${Math.round(k6Stats.sustained)}/s (HTTP accept)`);
  }
  console.log(`submit sustained: ${Math.round(m.submit_tx_per_sec_sustained)}/s  peak: ${Math.round(m.submit_tx_per_sec_peak)}/s  count: ${m.submit_count}`);
  console.log(`commit sustained: ${Math.round(m.commit_tx_per_sec_sustained)}/s  peak: ${Math.round(m.commit_tx_per_sec_peak)}/s  count: ${m.commit_count}`);
  console.log(`blocks: ${m.blocks_committed} (${Math.round(m.blocks_per_sec_sustained)}/s)  avg tx/block: ${Math.round(m.avg_tx_per_block || 0)}`);
  console.log(`e2e completed: ${m.e2e_completed}  pending: ${m.e2e_pending}  e2e peak: ${Math.round(m.e2e_tx_per_sec_peak)}/s`);
  console.log(`latency p50: ${Math.round(m.latency_ms_p50)} ms  p95: ${Math.round(m.latency_ms_p95)} ms  p99: ${Math.round(m.latency_ms_p99)} ms`);
  console.log(`RFP hints: submit≥5k=${m.meets_submit_sustained_5000}  commit≥5k=${m.meets_commit_sustained_5000}  p95<1s=${m.meets_latency_p95_under_1s}`);
}

export function teardown(data) {
  const waitSec = parseDurationSec(LEDGER_WAIT);
  const loadSec = parseDurationSec(DURATION);
  const loadEnd = new Date(new Date(data.loadStart).getTime() + loadSec * 1000).toISOString();

  if (waitSec > 0) {
    console.log(`chờ ledger ${LEDGER_WAIT}...`);
    sleep(waitSec);
  }

  const prefix = encodeURIComponent(TX_PREFIX);
  const untilNow = new Date().toISOString();

  const latest = fetchMetrics(data.baseUrl, `window=1&tx_prefix=${prefix}`);
  const peak = fetchMetrics(
    data.baseUrl,
    `mode=peak&lookback=240&window=1&tx_prefix=${prefix}`,
  );

  logMetrics('ledger latest (1s @ newest commit)', latest);
  logMetrics('ledger peak (best 1s in lookback)', peak);

  // Load window: submit during k6 run
  const benchLoad = fetchBenchmark(data.baseUrl, data.loadStart, loadEnd, prefix);
  logBenchmark('benchmark (load window)', benchLoad, null);

  // Extended window: include drain after load (E2E pending → 0)
  const benchDrain = fetchBenchmark(data.baseUrl, data.loadStart, untilNow, prefix);
  logBenchmark('benchmark (load + drain)', benchDrain, null);

  if (peak && latest && peak.tx_per_sec <= latest.tx_per_sec * 1.05) {
    console.log('gợi ý: peak ≈ latest — có thể đã chạm trần pipeline (~' + Math.round(peak.tx_per_sec) + ' tx/s)');
  }
}
