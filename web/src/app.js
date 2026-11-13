/* global React, ReactDOM, htm, TG, RECORDED, Go, hljs, JSZip */
const { useState, useEffect, useMemo, useRef, useCallback } = React;
const html = htm.bind(React.createElement);

const AREAS = {
  store: { title: 'Data', glaze: 'g-store', cap: 'store' },
  events: { title: 'Events', glaze: 'g-events', cap: 'event-bus' },
  api: { title: 'API', glaze: 'g-api', cap: 'api' },
  auth: { title: 'Sign-in', glaze: 'g-auth', cap: 'auth' },
  secrets: { title: 'Secrets', glaze: 'g-secrets', cap: 'secrets' },
};
const CAP_LABEL = { store: 'Store', 'event-bus': 'Event bus', api: 'API', auth: 'Sign-in', secrets: 'Secrets', transport: 'Transport' };
const DIMS = [
  ['llm-work', 'left for an LLM', '--d-llm'], ['maintenance', 'to maintain', '--d-maint'], ['dependency', 'dependencies', '--d-dep'],
  ['runtime', 'at run time', '--d-run'], ['operations', 'to operate', '--d-ops'], ['uncertainty', 'unproven', '--d-unc'],
];
const DIM_VAR = Object.fromEntries(DIMS.map(d => [d[0], d[2]]));

// ---------- the engine: tilegen itself, compiled to WebAssembly ----------
// It is the same compiler as the command line, so what the page shows is
// what `tilegen` writes. If it cannot load here, the page says so and keeps
// showing the example recorded from it.
async function loadEngine() {
  if (typeof Go === 'undefined' || typeof WebAssembly === 'undefined') throw new Error('WebAssembly is not available');
  const resp = await fetch('engine.wasm');
  if (!resp.ok) throw new Error('engine not found');
  let bytes = new Uint8Array(await resp.arrayBuffer());
  if (bytes[0] === 0x1f && bytes[1] === 0x8b) { // still gzipped: some hosts unpack it for us, some do not
    const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream('gzip'));
    bytes = new Uint8Array(await new Response(stream).arrayBuffer());
  }
  const go = new Go();
  const ready = new Promise(resolve => { window.tilegenReady = resolve; });
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(instance);
  await ready;
  return {
    compile: (spec, policy) => JSON.parse(window.tilegenCompile(spec, '', policy || '')),
    gomods: (files) => JSON.parse(window.tilegenGoMods(...files)),
  };
}

// A page served by `tilegen serve` can ask the server instead.
async function serverEngine() {
  const probe = await fetch('version');
  const v = probe.ok ? (await probe.json()).version : '';
  if (!v) throw new Error('no server');
  window.tilegenVersion = v;
  const post = async (path, body) => (await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json();
  return { compile: (spec, policy) => post('compile', { spec, policy }), gomods: (files) => post('gomods', { files }) };
}

const copyText = async (text) => { try { await navigator.clipboard.writeText(text); return true; } catch (e) { return false; } };
const lang = (path) => {
  const ext = path.split('.').pop();
  return { go: 'go', sql: 'sql', proto: 'protobuf', yaml: 'yaml', json: 'json', md: 'markdown', lock: 'lisp', sexp: 'lisp', mod: 'go' }[ext] || 'plaintext';
};
const escapeHTML = (s) => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
const paint = (line, language) => {
  if (!line) return ' ';
  try { if (window.hljs && hljs.getLanguage(language)) return hljs.highlight(line, { language, ignoreIllegals: true }).value; } catch (e) { /* plain */ }
  return escapeHTML(line);
};

function Tile({ g, kind }) { return html`<span className=${`tile ${g || ''} ${kind || ''}`} aria-hidden="true"></span>`; }
function Switch({ id, on, set, glaze, label }) {
  return html`<label className=${`sw ${glaze || ''}`}><input id=${id} type="checkbox" checked=${on} aria-label=${label} onChange=${e => set(e.target.checked)} /><span></span></label>`;
}

// ---------- 1 · the requirement sheet ----------
function Brief({ state, setState, presetId, setPresetId }) {
  const [text, setText] = useState('');
  const hits = useMemo(() => TG.match(text), [text]);
  const pending = hits.filter(h => {
    if (h.what === 'on') return !state[h.area].on;
    if (h.what === 'outbox' || h.what === 'inbox') return !state.events[h.what];
    if (h.what === 'watch') return !state.api.watch;
    return !state[h.area].reqs[h.what];
  });
  const label = (h) => h.what === 'on' ? AREAS[h.area].title.toLowerCase() : h.what;
  return html`<div className="card brief">
    <div className="hd"><h3>Start from a brief</h3></div>
    <div className="bd">
      <div className="presets" role="group" aria-label="Example briefs">
        ${TG.PRESETS.map(p => html`<button key=${p.id} className="chip" aria-pressed=${presetId === p.id}
          onClick=${() => { setState(JSON.parse(JSON.stringify(p.state))); setPresetId(p.id); setText(''); }}>${p.title}</button>`)}
      </div>
      ${presetId && !text ? html`<p className="hint">${TG.PRESETS.find(p => p.id === presetId).brief}</p>` : null}
      <label className="hint" htmlFor="brief-text">Or say it in your own words. Known phrases tick the boxes below; nothing is guessed.</label>
      <textarea id="brief-text" value=${text} placeholder="Orders must never be lost. Several instances. Staff sign in with SSO and a leaver is locked out at once."
        onChange=${e => setText(e.target.value)}></textarea>
      ${text.trim() ? html`<div className="hits">
        ${hits.length === 0 ? html`<span className="hint">No phrase recognised yet. Try words like restart, SSO, gRPC, audit, exactly once.</span>` : null}
        ${hits.map(h => html`<span key=${h.area + h.what} className="hit"><${Tile} g=${AREAS[h.area].glaze} />${label(h)} <q>${h.because}</q></span>`)}
      </div>` : null}
      ${hits.length ? html`<div className="row"><button className="btn" disabled=${!pending.length}
        onClick=${() => { setState(s => TG.apply(s, hits)); setPresetId(null); }}>${pending.length ? `Tick ${pending.length} box${pending.length > 1 ? 'es' : ''}` : 'All ticked'}</button></div>` : null}
    </div>
  </div>`;
}

function Service({ state, patch }) {
  const setField = (i, k, v) => patch(s => { s.fields[i][k] = v; });
  return html`<div className="card">
    <div className="hd"><${Tile} g="g-app" /><h3>The service</h3></div>
    <div className="bd">
      <div className="kv">
        <label htmlFor="svc-name">name</label><input id="svc-name" className="in" value=${state.name} onChange=${e => patch(s => { s.name = e.target.value; })} />
        <label htmlFor="svc-module">module</label><input id="svc-module" className="in" value=${state.module} onChange=${e => patch(s => { s.module = e.target.value; })} />
        <label htmlFor="svc-entity">it keeps</label><input id="svc-entity" className="in" value=${state.entity} onChange=${e => patch(s => { s.entity = e.target.value; })} />
      </div>
      <div className="fields">
        ${state.fields.map((f, i) => html`<${React.Fragment} key=${i}>
          <input id=${`f-n-${i}`} className="in" aria-label="field name" value=${f.n} onChange=${e => setField(i, 'n', e.target.value)} />
          <select id=${`f-t-${i}`} className="in" aria-label="field type" value=${f.t} onChange=${e => setField(i, 't', e.target.value)}>${TG.TYPES.map(t => html`<option key=${t}>${t}</option>`)}</select>
          <button className="x" aria-label=${`remove ${f.n}`} disabled=${f.n === 'ID'} onClick=${() => patch(s => { s.fields.splice(i, 1); })}>×</button>
        </${React.Fragment}>`)}
      </div>
      <div className="row"><button className="btn ghost" onClick=${() => patch(s => { s.fields.push({ n: 'Field' + (s.fields.length + 1), t: 'string' }); })}>Add a field</button></div>
    </div>
  </div>`;
}

function Check({ id, glaze, on, set, say, code, disabled }) {
  return html`<label className=${`ck ${glaze}`} htmlFor=${id}>
    <input id=${id} type="checkbox" checked=${!!on} disabled=${disabled} onChange=${e => set(e.target.checked)} />
    <span>${say}</span><code>${code}</code></label>`;
}

function Area({ area, state, patch, children, lead }) {
  const a = AREAS[area], st = state[area];
  return html`<div className=${`card ${st.on ? '' : 'offstate'}`}>
    <div className="hd"><${Tile} g=${a.glaze} kind=${st.on ? '' : 'legal'} /><h3>${a.title}</h3>
      <${Switch} id=${`on-${area}`} on=${st.on} glaze=${a.glaze} label=${`${a.title} on`} set=${v => patch(s => { s[area].on = v; })} /></div>
    ${st.on ? html`<div className="bd">
      ${lead}
      <div className="checks">
        ${TG.SHEET[area].map(q => html`<${Check} key=${q.req} id=${`${area}-${q.req}`} glaze=${a.glaze} on=${st.reqs[q.req]} say=${q.say} code=${q.req}
          set=${v => patch(s => { s[area].reqs[q.req] = v; })} />`)}
        ${children}
      </div>
    </div>` : null}
  </div>`;
}

// What the team already operates. A go.mod says so: a direct requirement
// on pgx means there is a Postgres somewhere. Reading a few of them ticks
// the engines found, and a tile on one of those costs nothing more to run.
function Running({ scan, running, setRunning, readGoMods, live }) {
  const [said, setSaid] = useState('');
  const [pasting, setPasting] = useState(false);
  const [pasted, setPasted] = useState('');
  const found = scan && scan.files;
  const engines = (scan && scan.engines) || [];
  const evidence = Object.fromEntries(((found && found.running) || []).map(r => [r.engine, r]));
  const read = async (texts) => {
    try { await readGoMods(texts); setSaid(''); } catch (e) { setSaid('Those files could not be read.'); }
  };
  const onFiles = async (e) => {
    const texts = await Promise.all(Array.from(e.target.files || []).map(f => f.text()));
    e.target.value = '';
    if (texts.length) read(texts);
  };
  const anyOn = engines.some(e => running[e]);
  return html`<div className=${`card ${anyOn ? '' : 'offstate'}`}>
    <div className="hd"><${Tile} g="g-app" kind=${anyOn ? '' : 'legal'} /><h3>What you already run</h3></div>
    <div className="bd">
      <p className="hint">A database or broker you already operate costs nothing more to run. Tick them, or give tilegen your services' <code>go.mod</code> files and it ticks what they show.</p>
      <div className="ops g-app" role="group" aria-label="engines already operated">
        ${engines.map(e => html`<label key=${e} className="op"><input id=${`run-${e}`} type="checkbox" checked=${!!running[e]}
          onChange=${ev => setRunning(r => ({ ...r, [e]: ev.target.checked }))} /><span>${e}${evidence[e] ? ` · ${evidence[e].in}/${found.modules.length}` : ''}</span></label>`)}
      </div>
      <div className="row">
        <label className="btn ghost filebtn">Read go.mod files<input id="gomod-files" type="file" multiple disabled=${!live} onChange=${onFiles} /></label>
        <button className="btn ghost" disabled=${!live} onClick=${() => setPasting(p => !p)}>${pasting ? 'Close' : 'Paste one'}</button>
      </div>
      ${pasting ? html`<${React.Fragment}>
        <textarea id="gomod-paste" className="in" style=${{ minHeight: 110, resize: 'vertical' }} spellCheck="false" placeholder=${'module github.com/you/billing\n\nrequire github.com/jackc/pgx/v5 v5.7.2'}
          value=${pasted} onChange=${e => setPasted(e.target.value)}></textarea>
        <div className="row"><button className="btn" disabled=${!pasted.trim()} onClick=${() => { read([pasted]); setPasting(false); }}>Read it</button></div>
      </${React.Fragment}>` : null}
      ${said ? html`<p className="hint" role="status">${said}</p>` : null}
      ${found ? html`<div className="gomods">
        <p className="hint">Read ${found.modules.length} module${found.modules.length === 1 ? '' : 's'}: ${found.modules.map(m => m.split('/').pop()).join(', ') || 'none'}.
          ${(found.errors || []).length ? ` ${found.errors.length} file${found.errors.length === 1 ? ' was' : 's were'} not a go.mod.` : ''}</p>
        ${(found.running || []).map(r => html`<div key=${r.engine} className="outrow"><${Tile} g="g-app" /><div><b style=${{ textDecoration: 'none' }}>${r.engine}</b>${" "}
          <span>because of ${r.modules.join(', ')}; shared by ${r.tiles.join(', ')}</span></div></div>`)}
        ${(found.running || []).length === 0 && found.modules.length ? html`<p className="hint">Nothing in them is a database or broker tilegen knows.</p>` : null}
        ${(found.unknown || []).length ? html`<details><summary className="hint">${found.unknown.length} requirement${found.unknown.length === 1 ? '' : 's'} no tile speaks</summary>
          <p className="mono" style=${{ fontSize: 12, overflowWrap: 'anywhere', color: 'var(--muted)' }}>${found.unknown.map(u => u.module).join(' · ')}</p></details>` : null}
      </div>` : null}
    </div>
  </div>`;
}

function Sheet({ state, setState, presetId, setPresetId, paused, resume, scan, running, setRunning, readGoMods, live }) {
  const patch = useCallback((fn) => { setState(s => { const n = JSON.parse(JSON.stringify(s)); fn(n); return n; }); setPresetId(null); }, [setState, setPresetId]);
  const E = TG.ident(state.entity, 'Item');
  const canSave = state.store.on && state.store.ops.save;
  return html`<section id="require" className="pane sheet">
    <header><span className="step">1</span><h2>What must be true</h2>
      <p>Tick what the system has to do. You never name a database or a broker: that is the part being decided.</p></header>
    ${paused ? html`<div className="note">You edited the spec by hand, so these boxes are paused. <button className="btn ghost" onClick=${resume}>Go back to the boxes</button></div>` : null}
    <div style=${paused ? { opacity: .45, pointerEvents: 'none' } : null}>
      <div style=${{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <${Brief} state=${state} setState=${setState} presetId=${presetId} setPresetId=${setPresetId} />
        <${Service} state=${state} patch=${patch} />
        <${Area} area="store" state=${state} patch=${patch} lead=${html`<div className="ops g-store" role="group" aria-label="store operations">
          ${['get', 'list', 'save', 'delete'].map(o => html`<label key=${o} className="op"><input id=${`op-${o}`} type="checkbox" checked=${!!state.store.ops[o]}
            onChange=${e => patch(s => { s.store.ops[o] = e.target.checked; })} /><span>${o}</span></label>`)}</div>`} />
        <${Area} area="events" state=${state} patch=${patch} lead=${html`<div className="kv"><label htmlFor="ev-name">event</label>
          <input id="ev-name" className="in" value=${state.events.name} onChange=${e => patch(s => { s.events.name = e.target.value; })} /></div>`}>
          <${Check} id="ev-outbox" glaze="g-events" on=${state.events.outbox && canSave} disabled=${!canSave} say=${`A ${E} is saved and its event published, or neither`} code="outbox"
            set=${v => patch(s => { s.events.outbox = v; })} />
          <${Check} id="ev-inbox" glaze="g-events" on=${state.events.inbox && state.store.on} disabled=${!state.store.on} say="Delivered twice, handled once" code="inbox"
            set=${v => patch(s => { s.events.inbox = v; })} />
        </${Area}>
        <${Area} area="api" state=${state} patch=${patch}>
          <${Check} id="api-watch" glaze="g-api" on=${state.api.watch && state.events.on} disabled=${!state.events.on} say="Clients watch events as a live stream" code="watch"
            set=${v => patch(s => { s.api.watch = v; })} />
        </${Area}>
        <${Area} area="auth" state=${state} patch=${patch} />
        <${Area} area="secrets" state=${state} patch=${patch} lead=${html`<div className="kv"><label htmlFor="sec-names">names</label>
          <input id="sec-names" className="in" value=${state.secrets.names} placeholder="DatabaseURL, SigningKey" onChange=${e => patch(s => { s.secrets.names = e.target.value; })} /></div>`} />
        <div className=${`card ${state.app ? '' : 'offstate'}`}>
          <div className="hd"><${Tile} g="g-app" kind=${state.app ? '' : 'legal'} /><h3>A program you can run</h3>
            <${Switch} id="on-app" on=${state.app} glaze="g-app" label="generate the program" set=${v => patch(s => { s.app = v; })} /></div>
          ${state.app ? html`<div className="bd"><p className="hint">Adds <code>cmd/${TG.pkgName(state.name)}</code>: every part built in dependency order, and a <code>main.go</code> to start it.</p></div>` : null}
        </div>
        <${Running} scan=${scan} running=${running} setRunning=${setRunning} readGoMods=${readGoMods} live=${live} />
        <div className="card">
          <div className="hd"><h3>What your team values</h3>
            <button className="btn ghost" disabled=${Object.keys(TG.DEFAULT_WEIGHTS).every(k => state.weights[k] === TG.DEFAULT_WEIGHTS[k])}
              onClick=${() => patch(s => { s.weights = { ...TG.DEFAULT_WEIGHTS }; })}>Reset</button></div>
          <div className="bd">
            <p className="hint">Every tile declares what it costs. These weights price those costs, so moving one can change a choice.</p>
            <div className="weights">
              ${DIMS.map(([k, label, v]) => html`<${React.Fragment} key=${k}>
                <label htmlFor=${`w-${k}`}><i style=${{ background: `var(${v})` }}></i>${label}</label>
                <input id=${`w-${k}`} type="range" min="0" max="8" step="1" value=${state.weights[k]} onChange=${e => patch(s => { s.weights[k] = +e.target.value; })} />
                <output htmlFor=${`w-${k}`}>${state.weights[k]}</output>
              </${React.Fragment}>`)}
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>`;
}

// ---------- 2 · the decisions ----------
function Bar({ cost, max }) {
  const total = cost.reduce((n, t) => n + t.value * t.weight, 0);
  return html`<div className="bar" style=${{ width: `${Math.max(6, (total / max) * 100)}%` }} role="img" aria-label=${cost.map(t => `${t.dimension} ${t.value}×${t.weight}`).join(', ')}>
    ${cost.filter(t => t.value * t.weight > 0).map(t => html`<span key=${t.dimension} title=${`${t.dimension}: ${t.value} × weight ${t.weight}${t.note ? ' · ' + t.note : ''}`}
      style=${{ flex: t.value * t.weight, background: `var(${DIM_VAR[t.dimension] || '--d-dep'})` }}></span>`)}
  </div>`;
}

function Need({ c }) {
  const glaze = 'g-' + c.capability;
  const max = Math.max(...c.candidates.map(x => x.score), 1);
  const short = c.need.split('.').pop();
  const winner = c.candidates.find(x => x.tile === c.chosen);
  const notes = (winner ? winner.cost : []).filter(t => t.note).slice(0, 2);
  const how = c.chosen_by === 'auto'
    ? (c.candidates.length > 1 ? `Cheapest of ${c.candidates.length} that can do it.` : 'The only tile that can do it.')
    : `Chosen by ${c.chosen_by}.`;
  return html`<article className=${`card need ${glaze}`}>
    <div className="hd">
      <div className="who">
        <span className="cap">${CAP_LABEL[c.capability] || c.capability}</span>
        <div className="chosen flip" key=${c.chosen}><${Tile} g=${glaze} /><strong>${c.chosen}</strong></div>
        <span className="name">${short} · ${how}</span>
      </div>
      <div className="score">${c.score}<small>cost</small></div>
    </div>
    <div className="bd">
      <div className="reqs">${(c.requirements || []).length
        ? c.requirements.map(r => html`<span key=${r} className="req">${r}</span>`)
        : html`<span className="req none">nothing asked: cheapest wins</span>`}</div>
      ${notes.length ? html`<p className="why">${notes.map(t => t.note[0].toUpperCase() + t.note.slice(1)).join('. ')}.</p>` : null}
      <div className="cands">
        ${c.candidates.map(x => html`<div key=${x.tile} className=${`cand ${x.tile === c.chosen ? 'win' : ''}`}>
          <${Tile} g=${glaze} kind=${x.tile === c.chosen ? '' : 'legal'} /><span className="nm">${x.tile}</span>
          <${Bar} cost=${x.cost || []} max=${max} /><span className="sc">${x.score}</span></div>`)}
      </div>
      ${(c.illegal || []).length ? html`<div className="outs"><h4>Ruled out</h4>
        ${c.illegal.map(x => html`<div key=${x.tile} className="outrow"><${Tile} kind="out" /><div><b>${x.tile}</b><span>${x.reason}</span></div></div>`)}
      </div>` : null}
    </div>
  </article>`;
}

// A refusal is an answer too: tilegen says which need nothing can cover and
// why each tile is out.
function Refusal({ error }) {
  const m = error.match(/no tile can cover (\S+) \(([^)]+)\)\n([\s\S]*)/);
  if (!m) return html`<div className="callout"><h3>tilegen cannot build this yet</h3><pre>${error}</pre></div>`;
  const rows = m[3].split('\n').map(l => l.trim()).filter(Boolean).map(l => { const i = l.indexOf(': '); return i < 0 ? null : [l.slice(0, i), l.slice(i + 2)]; }).filter(Boolean);
  return html`<div className="callout">
    <h3>Nothing can do all of that for ${m[1].split('.').pop()}</h3>
    <p className="hint" style=${{ color: 'inherit' }}>These requirements contradict each other. Untick one and a tile becomes legal again.</p>
    <ul>${rows.map(([t, why]) => html`<li key=${t}><b>${t}</b> ${why.replace(/^unverified: /, '')}</li>`)}</ul>
  </div>`;
}

function Decisions({ result, error, note }) {
  const cov = result ? result.explain.coverage : [];
  const files = result ? Object.keys(result.files).length : 0;
  const total = cov.reduce((n, c) => n + c.score, 0);
  return html`<section id="decide" className="pane decisions">
    <header><span className="step">2</span><h2>What tilegen chose, and why</h2>
      <p>Each need goes to the cheapest tile that can do everything asked of it. The rest are shown with the reason they lost.</p></header>
    ${error ? html`<${Refusal} error=${error} />` : null}
    ${note ? html`<div className="note">${note}</div>` : null}
    <div className=${error ? 'stale' : ''} style=${{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div className="summary">
        <span><b>${cov.length}</b>decision${cov.length === 1 ? '' : 's'}</span>
        <span><b>${total}</b>total cost</span>
        <span><b>${files}</b>files</span>
        <span><b>${result ? result.holes : 0}</b>holes left open</span>
      </div>
      <div className="legend" aria-label="what the cost bars are made of">${DIMS.map(([k, label, v]) => html`<span key=${k}><i style=${{ background: `var(${v})` }}></i>${label}</span>`)}</div>
      ${cov.map(c => html`<${Need} key=${c.need} c=${c} />`)}
    </div>
  </section>`;
}

// ---------- 3 · the output ----------
const HOLE = /tilegen:hole/;
function fileFacts(result) {
  const by = {}, tasks = {};
  for (const level of (result.plan && result.plan.levels) || []) for (const n of level) if (n.kind === 'file' && n.by) by[n.name] = n.by;
  try { for (const t of JSON.parse(result.files['tilegen.tasks.json']).tasks || []) (tasks[t.file] = tasks[t.file] || []).push(t); } catch (e) { /* none */ }
  const tileCap = {};
  for (const c of result.explain.coverage) tileCap[c.chosen] = c.capability;
  return { by, tasks, tileCap };
}
const ownership = (text) => /Code generated by tilegen\. DO NOT EDIT/.test(text.slice(0, 200)) ? 'generated'
  : /Scaffolded by tilegen/.test(text.slice(0, 200)) ? 'yours' : 'project';

function Files({ result, open, setOpen }) {
  const facts = useMemo(() => fileFacts(result), [result]);
  const paths = useMemo(() => Object.keys(result.files).sort((a, b) => {
    const da = a.includes('/') ? a.slice(0, a.lastIndexOf('/')) : '', db = b.includes('/') ? b.slice(0, b.lastIndexOf('/')) : '';
    return da === db ? a.localeCompare(b) : (da === '' ? -1 : db === '' ? 1 : da.localeCompare(db));
  }), [result]);
  const path = result.files[open] !== undefined ? open : (paths.find(p => /_gen\.go$/.test(p) && !/cmd\//.test(p)) || paths[0]);
  const text = result.files[path] || '';
  const [said, setSaid] = useState('');
  const owner = ownership(text), by = facts.by[path], tasks = facts.tasks[path] || [];
  const lines = useMemo(() => text.replace(/\n$/, '').split('\n').map(l => ({ html: paint(l, lang(path)), hole: HOLE.test(l) })), [text, path]);
  let lastDir = null;
  return html`<div className="files">
    <nav className="tree" aria-label="generated files">
      ${paths.map(p => {
        const dir = p.includes('/') ? p.slice(0, p.lastIndexOf('/')) : '';
        const head = dir !== lastDir ? html`<div key=${'d' + dir} className="dir">${dir || 'project root'}/</div>` : null;
        lastDir = dir;
        const holes = (facts.tasks[p] || []).length, cap = facts.tileCap[facts.by[p]];
        return html`<${React.Fragment} key=${p}>${head}<button aria-current=${p === path} onClick=${() => setOpen(p)}>
          <${Tile} g=${cap ? 'g-' + cap : 'g-app'} kind=${ownership(result.files[p]) === 'generated' ? '' : 'legal'} />
          <span>${p.split('/').pop()}</span>${holes ? html`<em title="holes to fill">${holes}</em>` : null}</button></${React.Fragment}>`;
      })}
    </nav>
    <div style=${{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 10 }}>
      <select id="file-pick" className="in filepick" aria-label="file" value=${path} onChange=${e => setOpen(e.target.value)}>
        ${paths.map(p => html`<option key=${p} value=${p}>${p}${(facts.tasks[p] || []).length ? `  (${facts.tasks[p].length} holes)` : ''}</option>`)}
      </select>
      <div className="viewer">
        <div className="vh"><span className="path">${path}</span>
          ${by ? html`<span className="badge"><${Tile} g=${'g-' + (facts.tileCap[by] || 'app')} />from ${by}</span>` : null}
          <span className=${`badge ${owner === 'yours' ? 'yours' : ''}`}>${owner === 'generated' ? 'regenerated every run' : owner === 'yours' ? 'yours after the first run' : 'project file'}</span>
          <button className="btn ghost" onClick=${async () => setSaid(await copyText(text) ? 'Copied' : 'Select the text to copy it')}>Copy</button>
        </div>
        ${tasks.length ? html`<div className="about">
          <span>${tasks.length} hole${tasks.length > 1 ? 's' : ''} here. Each is a typed method with its intent written down, for a person or an LLM to fill:</span>
          ${tasks.map(t => html`<span key=${t.id}><b>${t.symbol || t.id}</b> ${t.intent}</span>`)}
        </div>` : null}
        ${said ? html`<div className="about" role="status">${said}</div>` : null}
        <pre className="code" tabIndex="0" aria-label=${`contents of ${path}`}>${lines.map((l, i) => html`<span key=${i} className=${`l ${l.hole ? 'hole' : ''}`} dangerouslySetInnerHTML=${{ __html: l.html }}></span>`)}</pre>
      </div>
    </div>
  </div>`;
}

function Spec({ spec, policy, custom, setCustom }) {
  return html`<div style=${{ display: 'flex', flexDirection: 'column', gap: 10 }}>
    <p className="hint">This is everything tilegen is given. The boxes on the left write it; you can also edit it here, and the boxes pause until you go back.</p>
    <textarea id="spec-text" className="spec" spellCheck="false" aria-label="the spec" value=${custom === null ? spec : custom} onChange=${e => setCustom(e.target.value)}></textarea>
    ${policy ? html`<${React.Fragment}><p className="hint">And the policy your weights make:</p><pre className="code" style=${{ border: '1px solid var(--line)', borderRadius: 10, maxHeight: 200 }}>${policy.split('\n').map((l, i) => html`<span key=${i} className="l" dangerouslySetInnerHTML=${{ __html: paint(l, 'lisp') }}></span>`)}</pre></${React.Fragment}>` : null}
  </div>`;
}

const STEP_SAYS = {
  'sqlc generate': 'writes the database access package from the SQL',
  'buf generate': 'writes the protobuf and RPC code from the .proto',
  'go mod tidy': 'fetches the modules the chosen tiles use',
  'go build ./...': 'it compiles as generated; holes panic until they are filled',
};

function Build({ result, spec, openFile }) {
  const [said, setSaid] = useState('');
  const [saver, setSaver] = useState(undefined); // undefined: asking; null: this view cannot save
  useEffect(() => {
    let live = true;
    const ask = window.claude && window.claude.use ? window.claude.use('downloads').catch(() => null) : Promise.resolve(null);
    ask.then(d => { if (live) setSaver(d || null); });
    return () => { live = false; };
  }, []);
  const tasks = useMemo(() => { try { return JSON.parse(result.files['tilegen.tasks.json']).tasks || []; } catch (e) { return []; } }, [result]);
  const name = result.name || 'project';
  const zip = async () => {
    const z = new JSZip();
    for (const [p, t] of Object.entries(result.files)) z.file(`${name}/${p}`, t);
    z.file(`${name}/spec.sexp`, spec);
    return z.generateAsync({ type: 'blob' });
  };
  const download = async () => {
    setSaid('Packing…');
    try {
      const blob = await zip();
      if (saver) { await saver.save({ filename: `${name}.zip`, data: blob }); setSaid(`Saved ${name}.zip`); return; }
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob); a.download = `${name}.zip`; document.body.appendChild(a); a.click(); a.remove();
      setSaid(`Downloading ${name}.zip`);
    } catch (e) {
      setSaid(e && e.code === 'declined' ? 'Not saved.' : 'This view could not save the file. Copy the spec and run tilegen instead.');
    }
  };
  const canZip = typeof JSZip !== 'undefined' && saver !== undefined;
  return html`<div style=${{ display: 'flex', flexDirection: 'column', gap: 16 }}>
    <div className="card"><div className="hd"><h3>Take it with you</h3></div><div className="bd">
      <p className="hint">${Object.keys(result.files).length} files and the spec that made them, as <code>${name}.zip</code>.</p>
      <div className="row">
        ${canZip ? html`<button className="btn" onClick=${download}>Download ${name}.zip</button>` : null}
        <button className="btn ghost" onClick=${async () => setSaid(await copyText(spec) ? 'Spec copied. Save it as spec.sexp.' : 'Open the Spec tab and select the text to copy it')}>Copy the spec</button>
      </div>
      <div className="toast" role="status">${said}</div>
    </div></div>
    <div className="card"><div className="hd"><h3>Make it build</h3></div><div className="bd">
      <ol className="steps">
        <li><div><code>cd ${name}</code></div></li>
        ${(result.next || []).map(c => html`<li key=${c}><div><code>${c}</code><small>${STEP_SAYS[c] || ''}</small></div></li>`)}
      </ol>
      <p className="hint">Or regenerate it yourself any time: <code>brew install vinodhalaharvi/tap/tilegen</code>, then <code>tilegen -out ${name} spec.sexp</code>.</p>
    </div></div>
    <div className="card"><div className="hd"><h3>${tasks.length} hole${tasks.length === 1 ? '' : 's'} left open</h3></div><div className="bd">
      <p className="hint">Everything that follows from the spec is written. What needs judgment is a typed method with its intent recorded; <code>tilegen fill</code> hands each to an LLM, or you write it.</p>
      <div className="holes">${tasks.map(t => html`<button key=${t.id} className="holerow" onClick=${() => openFile(t.file)}><b>${t.id}</b><span>${t.intent}</span></button>`)}</div>
    </div></div>
  </div>`;
}

function Output({ result, spec, policy, custom, setCustom, tab, setTab, open, setOpen }) {
  const tabs = [['files', 'Files', result ? Object.keys(result.files).length : 0], ['spec', 'Spec', null], ['build', 'Build it', null]];
  return html`<section id="code" className="pane output">
    <header><span className="step">3</span><h2>What you get</h2>
      <p>Real Go, SQL and protobuf, written by tilegen in this page as you tick. Filled tiles are regenerated every run; outlined ones are yours to finish.</p></header>
    <div className="tabs" role="tablist">${tabs.map(([id, label, n]) => html`<button key=${id} role="tab" className="tab" aria-selected=${tab === id} onClick=${() => setTab(id)}>${label}${n ? html`<small>${n}</small>` : null}</button>`)}</div>
    ${!result ? html`<p className="hint">Nothing generated yet.</p>`
      : tab === 'files' ? html`<${Files} result=${result} open=${open} setOpen=${setOpen} />`
      : tab === 'spec' ? html`<${Spec} spec=${spec} policy=${policy} custom=${custom} setCustom=${setCustom} />`
      : html`<${Build} result=${result} spec=${custom === null ? spec : custom} openFile=${p => { setOpen(p); setTab('files'); }} />`}
  </section>`;
}

// ---------- the page ----------
function App({ boot }) {
  const first = TG.PRESETS[0];
  const [state, setState] = useState(boot.state || JSON.parse(JSON.stringify(first.state)));
  const [presetId, setPresetId] = useState(boot.state ? (boot.presetId || null) : first.id);
  const [custom, setCustom] = useState(boot.custom === undefined ? null : boot.custom);
  const [tab, setTab] = useState(boot.tab || 'files');
  const [open, setOpen] = useState(boot.open || '');
  const [engine, setEngine] = useState({ status: 'loading' });
  const [result, setResult] = useState(RECORDED);
  const [error, setError] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const compile = useRef(null);

  const built = useMemo(() => TG.buildSpec(state), [state]);
  const [running, setRunning] = useState(boot.running || {});
  const [scan, setScan] = useState(null); // what the uploaded go.mod files said
  const policy = useMemo(() => TG.buildPolicy(state, running), [state, running]);
  const readGoMods = useCallback(async (files) => {
    if (!compile.current) return;
    const r = await compile.current.gomods(files);
    setScan(prev => ({ ...r, asked: files.length || (prev && prev.asked) || 0, files: files.length ? r : prev }));
    if (files.length) setRunning(Object.fromEntries((r.running || []).map(x => [x.engine, true])));
  }, []);
  useEffect(() => { if (engine.status === 'live') readGoMods([]).catch(() => {}); }, [engine.status]);
  const spec = custom === null ? built : custom;

  useEffect(() => {
    let live = true;
    serverEngine().then(fn => ({ fn, where: 'server' })).catch(() => loadEngine().then(fn => ({ fn, where: 'page' })))
      .then(({ fn, where }) => { if (live) { compile.current = fn; setEngine({ status: 'live', where, version: window.tilegenVersion || '' }); } })
      .catch(() => { if (live) setEngine({ status: 'off' }); });
    return () => { live = false; };
  }, []);

  useEffect(() => {
    if (engine.status !== 'live') return;
    setBusy(true);
    const t = setTimeout(async () => {
      let r, added = false;
      try {
        r = await compile.current.compile(spec, policy);
        if (r.error && custom === null && TG.NEEDS_SESSION.test(r.error)) { // tilegen says what the chosen tile needs; give it that
          r = await compile.current.compile(TG.buildSpec(state, { session: true }), policy);
          added = true;
        }
      } catch (e) { r = { error: String(e && e.message || e) }; }
      setBusy(false);
      if (r.error) { setError(r.error.replace(/^(spec|config|policy)\.sexp:\d+:\d+: /, '')); return; }
      setError(''); setResult(r);
      setNote(added ? 'Signing in this way keeps sessions in a store, so a Session entity was added to the spec. tilegen asked for it by name.' : '');
    }, 140);
    return () => clearTimeout(t);
  }, [spec, policy, engine.status]);

  useEffect(() => {
    const hot = window.claude && window.claude.hot;
    if (hot && hot.snapshot) hot.snapshot(() => ({ state, presetId, custom, tab, open, running }));
  });

  const shown = result && result.version ? result.version : '';
  const pill = engine.status === 'live' ? `tilegen ${engine.version || shown} · running ${engine.where === 'server' ? 'on this machine' : 'in this page'}`
    : engine.status === 'loading' ? 'starting tilegen…' : `recorded from tilegen ${shown} · the live engine could not start here`;
  return html`<${React.Fragment}>
    <header className="top">
      <div className="brand">
        <div className="logo" aria-hidden="true"><${Tile} g="g-store" /><${Tile} g="g-events" /><${Tile} g="g-api" /><${Tile} g="g-auth" /><${Tile} g="g-secrets" /><span className="tile gap"></span></div>
        <h1 className="wordmark">tilegen <span>studio</span></h1>
      </div>
      <p className="thesis"><b>Say what must be true.</b> tilegen picks the tools that can do it at the lowest cost, writes the Go, and shows its reasoning.</p>
      ${engine.where === 'server' ? html`<a className="chip" href="docs">Docs</a>` : null}
      <span className=${`engine ${engine.status} ${busy ? 'busy' : ''}`} role="status"><i></i>${pill}</span>
    </header>
    <main className="bench">
      <${Sheet} state=${state} setState=${setState} presetId=${presetId} setPresetId=${setPresetId} paused=${custom !== null} resume=${() => setCustom(null)}
        scan=${scan} running=${running} setRunning=${setRunning} readGoMods=${readGoMods} live=${engine.status === 'live'} />
      <${Decisions} result=${result} error=${error} note=${note} />
      <${Output} result=${result} spec=${built} policy=${policy} custom=${custom} setCustom=${setCustom} tab=${tab} setTab=${setTab} open=${open} setOpen=${setOpen} />
    </main>
    <nav className="dock" aria-label="sections">
      <a href="#require"><b>1</b>Require</a><a href="#decide"><b>2</b>Decide</a><a href="#code"><b>3</b>Get the code</a>
    </nav>
  </${React.Fragment}>`;
}

const start = (boot) => ReactDOM.createRoot(document.getElementById('root')).render(html`<${App} boot=${boot || {}} />`);
if (window.claude && window.claude.hot && window.claude.hot.ready) window.claude.hot.ready(start); else start((window.claude && window.claude.hot && window.claude.hot.data) || {});
