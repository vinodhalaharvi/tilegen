// Everything the page knows that is not React: the requirement sheet, how it
// becomes a spec, and the phrases the description box listens for.
const TG = (() => {
  const TYPES = ['string', 'int64', 'int', 'bool', 'float64', 'time.Time', 'uuid.UUID', '[]byte'];

  // The requirement sheet. Each line is a sentence an architect would say;
  // `req` is the word tilegen knows it by.
  const SHEET = {
    store: [
      { req: 'durable', say: 'Data survives a restart' },
      { req: 'cross-process', say: 'Several instances share it' },
      { req: 'no-broker', say: 'Nothing else to install or operate' },
    ],
    events: [
      { req: 'cross-process', say: 'Other programs receive them' },
      { req: 'durable', say: 'An event outlives a crash' },
      { req: 'at-least-once', say: 'No event is ever lost' },
      { req: 'ordered', say: 'Handled in the order published' },
      { req: 'replay', say: 'A new consumer can rebuild from history' },
      { req: 'consumer-groups', say: 'Consumers scale out and share the work' },
      { req: 'fan-out', say: 'Every subscriber gets every event' },
      { req: 'no-broker', say: 'Nothing else to install or operate' },
    ],
    api: [
      { req: 'typed-clients', say: 'Clients are generated from a schema' },
      { req: 'browser-clients', say: 'A browser calls it directly' },
      { req: 'grpc-native', say: 'It must be grpc-go, interceptors and all' },
    ],
    auth: [
      { req: 'third-party-idp', say: 'People sign in through the company identity provider' },
      { req: 'revocable', say: 'Access can be cut off at once' },
      { req: 'expiring', say: 'Access lapses on its own' },
      { req: 'offline-verify', say: 'Checked without a network call' },
    ],
    secrets: [
      { req: 'rotating', say: 'A changed secret is seen without a restart' },
      { req: 'audited', say: 'Every read is on record' },
    ],
  };

  const blank = () => ({
    name: 'orders', module: 'github.com/you/orders',
    entity: 'Order',
    fields: [{ n: 'ID', t: 'uuid.UUID' }, { n: 'CustomerEmail', t: 'string' }, { n: 'TotalCents', t: 'int64' }, { n: 'PlacedAt', t: 'time.Time' }],
    store: { on: true, ops: { get: true, list: true, save: true, delete: false }, reqs: {} },
    events: { on: false, name: 'OrderPlaced', reqs: {}, outbox: false, inbox: false },
    api: { on: true, reqs: {}, watch: false },
    auth: { on: false, reqs: {} },
    secrets: { on: false, names: 'DatabaseURL', reqs: {} },
    app: true,
    weights: { 'llm-work': 4, maintenance: 3, dependency: 1, runtime: 1, operations: 3, uncertainty: 5 },
  });
  const DEFAULT_WEIGHTS = blank().weights;

  const set = (...names) => Object.fromEntries(names.map(n => [n, true]));
  const make = (patch) => {
    const s = blank();
    for (const k of Object.keys(patch)) s[k] = (s[k] && typeof s[k] === 'object' && !Array.isArray(s[k])) ? { ...s[k], ...patch[k] } : patch[k];
    return s;
  };

  // Five briefs, as an architect would hand them over.
  const PRESETS = [
    { id: 'ledger', title: 'Payments ledger',
      brief: 'No payment event may ever be lost. Several instances run at once. Every read of a credential is on record.',
      state: make({ name: 'ledger', module: 'github.com/you/ledger', entity: 'Payment',
        fields: [{ n: 'ID', t: 'uuid.UUID' }, { n: 'PayerEmail', t: 'string' }, { n: 'Cents', t: 'int64' }, { n: 'TakenAt', t: 'time.Time' }],
        store: { reqs: set('durable', 'cross-process') },
        events: { on: true, name: 'PaymentTaken', reqs: set('durable', 'at-least-once', 'cross-process'), outbox: true, inbox: true },
        api: { reqs: set('typed-clients'), watch: true },
        secrets: { on: true, names: 'DatabaseURL, NATSURL', reqs: set('audited') } }) },
    { id: 'kiosk', title: 'Shop-floor kiosk',
      brief: 'One box, nothing else to install or operate. Data survives a power cut. Scans are handled in order.',
      state: make({ name: 'kiosk', module: 'github.com/you/kiosk', entity: 'Scan',
        fields: [{ n: 'ID', t: 'string' }, { n: 'Barcode', t: 'string' }, { n: 'Quantity', t: 'int' }, { n: 'At', t: 'time.Time' }],
        store: { reqs: set('durable', 'no-broker') },
        events: { on: true, name: 'ItemScanned', reqs: set('ordered', 'no-broker') },
        api: { reqs: set('browser-clients') },
        secrets: { on: true, names: 'LicenseKey', reqs: {} } }) },
    { id: 'portal', title: 'Customer portal',
      brief: 'Web and mobile clients from one schema. Staff sign in through the company identity provider, and a leaver is locked out at once.',
      state: make({ name: 'portal', module: 'github.com/you/portal', entity: 'Ticket',
        fields: [{ n: 'ID', t: 'uuid.UUID' }, { n: 'Subject', t: 'string' }, { n: 'Open', t: 'bool' }, { n: 'OpenedAt', t: 'time.Time' }],
        store: { ops: { get: true, list: true, save: true, delete: true }, reqs: set('durable', 'cross-process') },
        api: { reqs: set('typed-clients', 'browser-clients') },
        auth: { on: true, reqs: set('third-party-idp', 'revocable') } }) },
    { id: 'feed', title: 'Activity feed',
      brief: 'A new consumer must be able to rebuild from history. Order is preserved. Consumers scale out and share the work.',
      state: make({ name: 'feed', module: 'github.com/you/feed', entity: 'View',
        fields: [{ n: 'ID', t: 'uuid.UUID' }, { n: 'ItemID', t: 'string' }, { n: 'At', t: 'time.Time' }],
        store: { ops: { get: true, list: true, save: true, delete: false }, reqs: set('durable', 'cross-process') },
        events: { on: true, name: 'ItemViewed', reqs: set('durable', 'replay', 'ordered', 'consumer-groups', 'cross-process') },
        api: { reqs: set('typed-clients'), watch: true } }) },
    { id: 'pricing', title: 'Pricing service',
      brief: 'Service-to-service gRPC with our interceptors. Tokens are verified without a network call and expire. Keys rotate without a restart.',
      state: make({ name: 'pricing', module: 'github.com/you/pricing', entity: 'Price',
        fields: [{ n: 'ID', t: 'string' }, { n: 'SKU', t: 'string' }, { n: 'Cents', t: 'int64' }, { n: 'ValidFrom', t: 'time.Time' }],
        store: { ops: { get: true, list: false, save: true, delete: false }, reqs: set('durable', 'cross-process') },
        api: { reqs: set('grpc-native') },
        auth: { on: true, reqs: set('offline-verify', 'expiring') },
        secrets: { on: true, names: 'DatabaseURL, SigningKey', reqs: set('rotating') } }) },
  ];

  const ident = (s, fallback) => {
    const w = String(s || '').replace(/[^A-Za-z0-9_]/g, '');
    if (!w || !/^[A-Za-z]/.test(w)) return fallback;
    return w[0].toUpperCase() + w.slice(1);
  };
  const pkgName = (s) => (String(s || '').toLowerCase().replace(/[^a-z0-9]/g, '').replace(/^[0-9]+/, '') || 'app');
  const on = (reqs) => Object.keys(reqs || {}).filter(k => reqs[k]);
  const snake = (s) => s.replace(/([a-z0-9])([A-Z])/g, '$1_$2').replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2').toLowerCase();

  // secretList reads "DatabaseURL, api token" as secrets, giving each a key
  // where the name would split badly (NATSURL -> natsurl, not n_a_t_s_u_r_l).
  const secretList = (text) => {
    const seen = new Set(), out = [];
    for (const raw of String(text || '').split(/[\s,;]+/)) {
      const name = ident(raw, '');
      if (!name || seen.has(name)) continue;
      seen.add(name);
      const auto = snake(name);
      const tidy = /(^|_)[a-z0-9](_[a-z0-9])+(_|$)/.test(auto) || /[A-Z]{2,}[A-Z][a-z]|[A-Z]{3,}$/.test(name)
        ? name.replace(/URL$/, '_URL').replace(/ID$/, '_ID').replace(/([a-z0-9])([A-Z])/g, '$1_$2').replace(/__+/g, '_').toLowerCase() : '';
      out.push({ name, key: tidy && tidy !== auto ? tidy : '' });
    }
    return out;
  };

  // buildSpec writes the S-expression the sheet stands for. What the sheet
  // cannot mean is left out here rather than sent to fail: an api with
  // nothing to serve, an outbox with nothing to save.
  // close adds closing parentheses to the last line, before its comment.
  const close = (L, parens) => {
    const m = L[L.length - 1].match(/^(.*?)(\s*;.*)?$/);
    L[L.length - 1] = m[1] + parens + (m[2] || '');
  };

  function buildSpec(s, opt = {}) {
    const pkg = pkgName(s.name), E = ident(s.entity, 'Item');
    const fields = [], names = new Set();
    for (const f of s.fields) {
      const n = ident(f.n, '');
      if (!n || names.has(n)) continue;
      names.add(n);
      fields.push({ n, t: TYPES.includes(f.t) ? f.t : 'string' });
    }
    if (!names.has('ID')) fields.unshift({ n: 'ID', t: 'string' });
    const idType = fields.find(f => f.n === 'ID').t;
    const usesUUID = fields.some(f => f.t === 'uuid.UUID');
    const L = [];
    const hasStore = s.store.on;
    const storeOps = ['get', 'list', 'save', 'delete'].filter(o => s.store.ops[o]);
    const ev = s.events.on, Ev = ident(s.events.name, E + 'Changed');
    const canSave = hasStore && s.store.ops.save;
    const outbox = ev && s.events.outbox && canSave;
    const inbox = ev && s.events.inbox && hasStore;

    L.push(`(project ${pkg}`, `  (module ${String(s.module || '').trim() || 'github.com/you/' + pkg})`, '  (go 1.22)');
    if (usesUUID) L.push('  (require (uuid github.com/google/uuid v1.6.0))');
    if (s.app) L.push('  (app)                         ; the program too: cmd/' + pkg);
    L.push('', `  (package ${pkg}`, '');
    L.push(`    (entity ${E}`);
    for (const f of fields) L.push(`      (field ${f.n} ${f.t})`);
    if (hasStore) {
      const reqs = on(s.store.reqs).map(r => ` (${r})`).join('');
      L.push(`      (store ${storeOps.length ? storeOps.join(' ') : 'get'}${reqs}))`);
    } else close(L, ')');
    if (opt.session) { // the session-store tile keeps its sessions in a store of the package
      L.push('', '    (entity Session', '      (field ID string)', '      (field Subject string)', '      (field ExpiresAt time.Time)', '      (store get save delete (durable)))');
    }
    if (ev) {
      L.push('', '    (events');
      for (const r of on(s.events.reqs)) L.push(`      (${r})`);
      L.push(`      (event ${Ev} (field ${E}ID ${idType}))`);
      if (outbox) L.push(`      (outbox ${E})               ; saved and published, or neither`);
      if (inbox) L.push(`      (inbox ${Ev} ${E}ID)   ; delivered twice, handled once`);
      close(L, ')');
    }
    if (s.auth.on) {
      L.push('', '    (auth');
      const reqs = on(s.auth.reqs);
      reqs.forEach(r => L.push(`      (${r})`));
      close(L, ')');
    }
    const apiOps = hasStore ? storeOps : [];
    const watch = s.api.watch && ev;
    if (s.api.on && (apiOps.length || watch)) {
      L.push('', '    (api');
      if (apiOps.length) L.push('      ' + apiOps.map(o => `(${o} ${E})`).join(' '));
      if (watch) L.push(`      (watch ${Ev})`);
      for (const r of on(s.api.reqs)) L.push(`      (${r})`);
      close(L, ')');
    }
    const secrets = s.secrets.on ? secretList(s.secrets.names) : [];
    if (secrets.length) {
      L.push('', '    (secrets');
      for (const x of secrets) L.push(`      (secret ${x.name}${x.key ? ` (key ${x.key})` : ''})`);
      for (const r of on(s.secrets.reqs)) L.push(`      (${r})`);
      close(L, ')');
    }
    close(L, '))');
    return L.join('\n') + '\n';
  }

  // buildPolicy prices the cost dimensions the way the sliders say. The
  // default weights need no policy at all.
  function buildPolicy(s, running) {
    const w = s.weights || DEFAULT_WEIGHTS;
    const parts = [];
    if (!Object.keys(DEFAULT_WEIGHTS).every(k => w[k] === DEFAULT_WEIGHTS[k])) {
      parts.push('  (weights\n' + Object.keys(DEFAULT_WEIGHTS).map(k => `    (${k} ${w[k]})`).join('\n') + ')');
    }
    const run = Object.keys(running || {}).filter(e => running[e]).sort();
    if (run.length) parts.push(`  (running ${run.join(' ')})   ; already operated: nothing more to run`);
    if (!parts.length) return '';
    const last = parts.length - 1;
    parts[last] = parts[last].replace(/^(.*?)(\s*;.*)?$/, (_, a, c) => a + ')' + (c || ''));
    return '(policy\n' + parts.join('\n') + '\n';
  }

  // The phrases the description box listens for. Plain matching, no model:
  // each pattern ticks the boxes beside it.
  const PHRASES = [
    [/surviv|restart|persist|durabl|power cut|not lose data|kept safe/i, [['store', 'durable']]],
    [/several instances|multiple instances|replicas|scale[sd]? out|horizontal|cluster|more than one (copy|instance)/i, [['store', 'cross-process']]],
    [/embedded|single binary|one box|no servers?\b|nothing (else )?to (install|operate|run)|edge device|kiosk/i, [['store', 'no-broker'], ['events', 'no-broker']]],
    [/\bevents?\b|publish|message bus|queue|notif/i, [['events', 'on']]],
    [/other (programs|services)|between services|across services|microservice/i, [['events', 'on'], ['events', 'cross-process']]],
    [/at.least.once|never (be )?lost|must not be lost|no .{0,20}(may|can|must) .{0,12}lost|guaranteed delivery/i, [['events', 'on'], ['events', 'at-least-once'], ['events', 'durable']]],
    [/in order|ordered|ordering|sequence/i, [['events', 'on'], ['events', 'ordered']]],
    [/replay|rebuild from history|event.sourc|from the beginning/i, [['events', 'on'], ['events', 'replay'], ['events', 'durable']]],
    [/consumer.groups?|share the work|competing consumers|workers? pool/i, [['events', 'on'], ['events', 'consumer-groups']]],
    [/fan.?out|every subscriber|broadcast/i, [['events', 'on'], ['events', 'fan-out']]],
    [/atomic|outbox|both or neither|same transaction|save and publish/i, [['events', 'on'], ['events', 'outbox']]],
    [/exactly.once|idempoten|handled once|duplicat|twice/i, [['events', 'on'], ['events', 'inbox']]],
    [/browser|web app|front.?end|single.page|\bspa\b/i, [['api', 'on'], ['api', 'browser-clients']]],
    [/typed clients?|generated clients?|\bsdk\b|mobile|schema|protobuf|\.proto\b/i, [['api', 'on'], ['api', 'typed-clients']]],
    [/grpc/i, [['api', 'on'], ['api', 'grpc-native']]],
    [/stream|live updates?|real.?time|\bwatch\b|push updates/i, [['api', 'on'], ['api', 'watch'], ['events', 'on']]],
    [/\brest\b|\bhttp\b|json api|\bapi\b|endpoint/i, [['api', 'on']]],
    [/sign.?in|log.?in|authenticat|\bauth\b|\bsso\b|staff|users? must/i, [['auth', 'on']]],
    [/\bsso\b|identity provider|\bidp\b|okta|entra|azure ad|google workspace|oidc/i, [['auth', 'on'], ['auth', 'third-party-idp']]],
    [/revok|log.?out|leaver|locked out|cut off|at once/i, [['auth', 'on'], ['auth', 'revocable']]],
    [/expir|lapse|time.?out/i, [['auth', 'on'], ['auth', 'expiring']]],
    [/without a network call|offline verif|stateless token|\bjwt\b/i, [['auth', 'on'], ['auth', 'offline-verify']]],
    [/secret|credential|password|api key|signing key|database url/i, [['secrets', 'on']]],
    [/rotat/i, [['secrets', 'on'], ['secrets', 'rotating']]],
    [/audit|on record|who read/i, [['secrets', 'on'], ['secrets', 'audited']]],
  ];

  // match reads a description and returns what it would tick, with the words
  // that caused each tick.
  function match(text) {
    const hits = [];
    for (const [re, ticks] of PHRASES) {
      const m = String(text || '').match(re);
      if (m) for (const [area, what] of ticks) hits.push({ area, what, because: m[0] });
    }
    const seen = new Set();
    return hits.filter(h => !seen.has(h.area + h.what) && seen.add(h.area + h.what));
  }

  // NEEDS_SESSION is how tilegen says the auth tile it chose keeps sessions
  // in a store the package does not have. The page adds one and says so.
  const NEEDS_SESSION = /session-store tile keeps sessions in a SessionStore/;

  function apply(state, hits) {
    const s = JSON.parse(JSON.stringify(state));
    for (const h of hits) {
      if (h.what === 'on') s[h.area].on = true;
      else if (h.what === 'outbox' || h.what === 'inbox') s.events[h.what] = true;
      else if (h.what === 'watch') s.api.watch = true;
      else s[h.area].reqs[h.what] = true;
    }
    return s;
  }

  return { NEEDS_SESSION, TYPES, SHEET, PRESETS, DEFAULT_WEIGHTS, blank, buildSpec, buildPolicy, match, apply, ident, pkgName, secretList };
})();
if (typeof module !== 'undefined') module.exports = TG;
