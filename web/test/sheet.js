// Runs the studio's requirement sheet against the real compiler.
//
//   make check-web     (builds tilegen for js/wasm, then runs this under node)
//
// The sheet writes a spec and a policy from ticked boxes. Whatever is
// ticked, tilegen must either build it or refuse it for a reason about the
// requirements. A parse error would mean the sheet wrote something that is
// not a spec, which is a bug in the page and never the reader's doing.
const fs = require('fs'), path = require('path');
const [wasmExec, wasm] = process.argv.slice(2);
require(path.resolve(wasmExec));
const TG = require('../src/core.js');

const REFUSALS = [/^no tile can cover /]; // contradictory requirements: an answer, not a fault
let failures = 0;
const fail = (what, detail) => { failures++; console.error('FAIL', what, '\n' + detail + '\n'); };

globalThis.tilegenReady = () => {
  const compile = (s, running, opt) => JSON.parse(globalThis.tilegenCompile(TG.buildSpec(s, opt), '', TG.buildPolicy(s, running)));
  const check = (label, s, running) => {
    let r = compile(s, running);
    if (r.error && TG.NEEDS_SESSION.test(r.error)) r = compile(s, running, { session: true });
    const msg = (r.error || '').replace(/^(spec|config|policy)\.sexp:\d+:\d+: /, '');
    if (r.error && !REFUSALS.some(re => re.test(msg))) fail(label, r.error + '\n' + TG.buildSpec(s) + TG.buildPolicy(s, running));
    return r;
  };

  // Every brief builds as it stands.
  for (const p of TG.PRESETS) { const r = check('brief: ' + p.title, p.state, {}); if (r.error) fail('brief refused: ' + p.title, r.error); }
  if (check('blank sheet', TG.blank(), {}).error) fail('the blank sheet is refused', '');

  // Every control, one at a time, on every brief: weights, engines, each box.
  const engines = JSON.parse(globalThis.tilegenGoMods()).engines;
  if (!engines.length) fail('no engines offered', '');
  for (const p of TG.PRESETS) {
    for (const dim of Object.keys(TG.DEFAULT_WEIGHTS)) for (const v of [0, 8]) {
      const s = JSON.parse(JSON.stringify(p.state)); s.weights[dim] = v;
      check(`${p.id}: weight ${dim}=${v}`, s, {});
      check(`${p.id}: weight ${dim}=${v}, running`, s, { [engines[0]]: true });
    }
    for (const e of engines) check(`${p.id}: running ${e}`, p.state, { [e]: true });
    check(`${p.id}: running all`, p.state, Object.fromEntries(engines.map(e => [e, true])));
  }

  // Then at random: 3,000 sheets with anything ticked.
  let seed = 20261113;
  const rnd = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
  let built = 0, refused = 0;
  for (let i = 0; i < 3000; i++) {
    const s = TG.blank();
    for (const area of Object.keys(TG.SHEET)) { s[area].on = rnd() < 0.7; for (const q of TG.SHEET[area]) if (rnd() < 0.3) s[area].reqs[q.req] = true; }
    for (const o of ['get', 'list', 'save', 'delete']) s.store.ops[o] = rnd() < 0.6;
    s.events.outbox = rnd() < 0.3; s.events.inbox = rnd() < 0.3; s.api.watch = rnd() < 0.4; s.app = rnd() < 0.5;
    if (rnd() < 0.3) s.secrets.names = ['DatabaseURL, NATSURL', 'api key; OAuthClientID', '  ', 'x'][Math.floor(rnd() * 4)];
    if (rnd() < 0.2) s.fields.push({ n: ['Note', 'note text', '9lives', ''][Math.floor(rnd() * 4)], t: TG.TYPES[Math.floor(rnd() * TG.TYPES.length)] });
    if (rnd() < 0.1) { s.name = ['My Service', '', '42'][Math.floor(rnd() * 3)]; s.entity = ['line item', '', 'x'][Math.floor(rnd() * 3)]; s.events.name = ['', 'thing happened'][Math.floor(rnd() * 2)]; }
    if (rnd() < 0.4) for (const d of Object.keys(TG.DEFAULT_WEIGHTS)) if (rnd() < 0.4) s.weights[d] = Math.floor(rnd() * 9);
    const running = {}; for (const e of engines) if (rnd() < 0.2) running[e] = true;
    check('random sheet ' + i, s, running).error ? refused++ : built++;
  }

  // The phrase box only ticks boxes, so whatever it reads must also hold.
  for (const text of ['payments must never be lost, several instances, audited, exactly once', 'kiosk, one box, ordered', 'browser and mobile, SSO, leaver locked out at once', 'grpc with jwt, keys rotate', 'live updates, rebuild from history, save and publish atomically']) {
    const hits = TG.match(text);
    if (!hits.length) fail('no phrase recognised in: ' + text, '');
    check('phrases: ' + text, TG.apply(TG.blank(), hits), {});
  }
  console.log(`sheet: ${built} built, ${refused} refused for contradictory requirements, ${failures} failures`);
  process.exit(failures ? 1 : 0);
};
const go = new Go();
WebAssembly.instantiate(fs.readFileSync(wasm), go.importObject).then(r => go.run(r.instance));
