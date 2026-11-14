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
    compile: (spec, policy, tiles) => JSON.parse(window.tilegenCompile(spec, '', policy || '', tiles || '')),
    gomods: (files) => JSON.parse(window.tilegenGoMods(...files)),
    tiles: async () => JSON.parse(window.tilegenTiles()),
    llm: { enabled: false, why: 'this page runs tilegen by itself, with no key: start `tilegen serve` on your machine to ask Claude' },
  };
}

// A page served by `tilegen serve` can ask the server instead.
async function serverEngine() {
  const probe = await fetch('version');
  const info = probe.ok ? await probe.json().catch(() => ({})) : {};
  if (!info.version) throw new Error('no server');
  window.tilegenVersion = info.version;
  const post = async (path, body) => (await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })).json();
  return {
    compile: (spec, policy, tiles) => post('compile', { spec, policy, tiles }),
    gomods: (files) => post('gomods', { files }),
    draft: (modules) => post('draft', { modules }),
    tiles: async () => (await fetch('tiles', { headers: { Accept: 'application/json' } })).json(),
    llm: info.llm || { enabled: false, why: 'this server does not offer it' },
  };
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
function Brief({ state, setState, presetId, setPresetId, off, setOff }) {
  const [text, setText] = useState('');
  const hits = useMemo(() => TG.match(text), [text]);
  const pending = hits.filter(h => {
    if (h.what === 'on') return !state[h.area].on;
    if (h.what === 'outbox' || h.what === 'inbox') return !state.events[h.what];
    if (h.what === 'watch') return !state.api.watch;
    return !state[h.area].reqs[h.what];
  });
  const isClear = useMemo(() => JSON.stringify(TG.clear(state)) === JSON.stringify(state) && !Object.keys(off).some(t => off[t]), [state, off]);
  const label = (h) => h.what === 'on' ? AREAS[h.area].title.toLowerCase() : h.what;
  return html`<div className="card brief">
    <div className="hd"><h3>Start from a brief</h3>
      <button className="btn ghost small" disabled=${isClear} onClick=${() => { setState(s => TG.clear(s)); setPresetId(null); setOff({}); }}>Clear all</button></div>
    <div className="bd">
      <div className="presets" role="group" aria-label="Example briefs">
        ${TG.PRESETS.map(p => html`<button key=${p.id} className="chip" aria-pressed=${presetId === p.id}
          onClick=${() => { setState(JSON.parse(JSON.stringify(p.state))); setPresetId(p.id); setText(''); }}>${p.title}</button>`)}
      </div>
      ${presetId && !text ? html`<p className="hint">${TG.PRESETS.find(p => p.id === presetId).brief}</p>` : null}
      <label className="hint" htmlFor="brief-text">Or say it in your own words. Known phrases tick the boxes below, on top of what is already ticked; nothing is guessed.</label>
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
function Running({ scan, running, setRunning, readGoMods, live, llm, drafts, setDrafts, askClaude }) {
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
      </div>` : null}
      <${Drafts} found=${found} llm=${llm} drafts=${drafts} setDrafts=${setDrafts} askClaude=${askClaude} />
    </div>
  </div>`;
}

// Tiles for this visit. Claude drafts one for each library tilegen does not
// know; a person reads it, changes what is wrong, and decides whether it
// takes part. Nothing counts until it is ticked.
function Drafts({ found, llm, drafts, setDrafts, askClaude }) {
  const [use, setUse] = useState(false);
  const [busy, setBusy] = useState(false);
  const [said, setSaid] = useState('');
  const unknown = ((found && found.unknown) || []).slice(0, 25);
  const ask = async () => {
    setBusy(true); setSaid('');
    try {
      const r = await askClaude(unknown.map(u => ({ module: u.module, version: u.version })));
      const n = r.drafts.filter(d => d.tile).length;
      setSaid(`${n} draft${n === 1 ? '' : 's'} from ${r.model}. Read each one before you tick it: it is Claude's reading of the library, and nothing has run it.`);
    } catch (e) { setSaid(String(e.message || e)); }
    setBusy(false);
  };
  const patch = (id, change) => setDrafts(ds => ds.map(d => d.id === id ? { ...d, ...change } : d));
  return html`<div className="gomods">
    ${unknown.length ? html`<${React.Fragment}>
      <p className="hint">${found.unknown.length} requirement${found.unknown.length === 1 ? '' : 's'} no tile speaks:
        <span className="mono" style=${{ overflowWrap: 'anywhere' }}> ${found.unknown.map(u => u.module).join(' · ')}</span></p>
      <label className="ck g-app" htmlFor="use-claude">
        <input id="use-claude" type="checkbox" checked=${use && llm.enabled} disabled=${!llm.enabled} onChange=${e => setUse(e.target.checked)} />
        <span>Also ask Claude what these are, and draft a tile for each
          <small className="hint" style=${{ display: 'block' }}>${llm.enabled
            ? `Sends ${unknown.length} module name${unknown.length === 1 ? '' : 's'} and version${unknown.length === 1 ? '' : 's'} to Anthropic, using the key on this machine. One request each.`
            : 'Off: ' + llm.why + '.'}</small></span><code>optional</code></label>
      ${use && llm.enabled ? html`<div className="row"><button className="btn" disabled=${busy} onClick=${ask}>${busy ? 'Asking Claude…' : `Ask about ${unknown.length}`}</button></div>` : null}
    </${React.Fragment}>` : null}
    ${said ? html`<p className="hint" role="status">${said}</p>` : null}
    ${drafts.map(d => html`<div key=${d.id} className="draft">
      <div className="row"><b className="mono" style=${{ fontSize: 12, overflowWrap: 'anywhere', flex: 1 }}>${d.module || 'your tile'}</b>
        <button className="x" aria-label="discard this draft" onClick=${() => setDrafts(ds => ds.filter(x => x.id !== d.id))}>×</button></div>
      ${d.note ? html`<p className="hint">${d.note}</p>` : null}
      ${d.text || !d.note ? html`<${React.Fragment}>
        <textarea id=${`draft-${d.id}`} className="in" spellCheck="false" aria-label=${`draft tile for ${d.module}`} style=${{ minHeight: 150, resize: 'vertical', whiteSpace: 'pre', fontSize: 12 }}
          value=${d.text} onChange=${e => patch(d.id, { text: e.target.value })}></textarea>
        <label className="ck g-app" htmlFor=${`draft-on-${d.id}`}><input id=${`draft-on-${d.id}`} type="checkbox" checked=${d.on} onChange=${e => patch(d.id, { on: e.target.checked })} />
          <span>Let it take part in the decisions</span><code>draft</code></label>
      </${React.Fragment}>` : null}
    </div>`)}
    <div className="row"><button className="btn ghost" onClick=${() => setDrafts(ds => ds.concat({ id: 'mine-' + Date.now(), module: '', on: false, note: '',
      text: '(tile my-store\n  (offers store)\n  (doc "what it is")\n  (satisfies (durable) "how")\n  (cost (llm-work 4) (maintenance 3) (dependency 2) (runtime 1) (operations 0)))' }))}>Write a tile yourself</button></div>
  </div>`;
}

function Sheet({ state, setState, presetId, setPresetId, off, setOff, paused, resume, scan, running, setRunning, readGoMods, live, llm, drafts, setDrafts, askClaude }) {
  const patch = useCallback((fn) => { setState(s => { const n = JSON.parse(JSON.stringify(s)); fn(n); return n; }); setPresetId(null); }, [setState, setPresetId]);
  const E = TG.ident(state.entity, 'Item');
  const canSave = state.store.on && state.store.ops.save;
  return html`<section id="require" className="pane sheet">
    <header><span className="step">1</span><h2>What must be true</h2>
      <p>Tick what the system has to do. You never name a database or a broker: that is the part being decided.</p></header>
    ${paused ? html`<div className="note">You edited the spec by hand, so these boxes are paused. <button className="btn ghost" onClick=${resume}>Go back to the boxes</button></div>` : null}
    <div style=${paused ? { opacity: .45, pointerEvents: 'none' } : null}>
      <div style=${{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <${Brief} state=${state} setState=${setState} presetId=${presetId} setPresetId=${setPresetId} off=${off} setOff=${setOff} />
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
        <${Running} scan=${scan} running=${running} setRunning=${setRunning} readGoMods=${readGoMods} live=${live}
          llm=${llm} drafts=${drafts} setDrafts=${setDrafts} askClaude=${askClaude} />
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

function Need({ c, inspect }) {
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
        <button className="chosen flip tlink" key=${c.chosen} title="What is behind this tile" onClick=${() => inspect(c.chosen)}><${Tile} g=${glaze} /><strong>${c.chosen}</strong></button>
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
          <${Tile} g=${glaze} kind=${x.tile === c.chosen ? '' : 'legal'} /><button className="nm tlink" onClick=${() => inspect(x.tile)}>${x.tile}${x.draft ? html` <em className="draftmark">draft</em>` : null}</button>
          <${Bar} cost=${x.cost || []} max=${max} /><span className="sc">${x.score}</span></div>`)}
      </div>
      ${(c.illegal || []).length ? html`<div className="outs"><h4>Ruled out</h4>
        ${c.illegal.map(x => html`<div key=${x.tile} className="outrow"><${Tile} kind="out" /><div><button className="tlink" onClick=${() => inspect(x.tile)}><b>${x.tile}</b></button><span>${x.reason}</span></div></div>`)}
      </div>` : null}
    </div>
  </article>`;
}

// A refusal is an answer too: tilegen says which need nothing can cover and
// why each tile is out.
function Refusal({ error, off, setOff }) {
  const no = Object.keys(off).filter(t => off[t]).sort();
  const backOn = no.length ? html`<div className="backon"><span>Switched off by you:</span>${no.map(t => html`<button key=${t} className="btn ghost small" onClick=${() => setOff(o => ({ ...o, [t]: false }))}>Switch ${t} back on</button>`)}</div>` : null;
  const m = error.match(/no tile can cover (\S+) \(([^)]+)\)\n([\s\S]*)/);
  if (/^tiles\.sexp/.test(error)) return html`<div className="callout"><h3>A draft tile has a mistake</h3><pre>${error.replace(/tiles\.sexp:/g, 'line ')}</pre></div>`;
  if (!m) return html`<div className="callout"><h3>tilegen cannot build this yet</h3><pre>${error}</pre>${backOn}</div>`;
  const rows = m[3].split('\n').map(l => l.trim()).filter(Boolean).map(l => { const i = l.indexOf(': '); return i < 0 ? null : [l.slice(0, i), l.slice(i + 2)]; }).filter(Boolean);
  return html`<div className="callout">
    <h3>Nothing can do all of that for ${m[1].split('.').pop()}</h3>
    <p className="hint" style=${{ color: 'inherit' }}>These requirements contradict each other. Untick one and a tile becomes legal again.</p>
    <ul>${rows.map(([t, why]) => html`<li key=${t}><b>${t}</b> ${why.replace(/^unverified: /, '')}</li>`)}</ul>
    ${backOn}
  </div>`;
}

// What an LLM would have been paid to write the same files. Tokens are
// estimated at four characters each; the price is Claude Sonnet 5.5 output.
const USD_PER_MTOK = 10;
function saving(result) {
  if (!result) return { tokens: 0, usd: 0 };
  let chars = 0;
  for (const p of Object.keys(result.files)) chars += result.files[p].length;
  const tokens = Math.round(chars / 4);
  return { tokens, usd: tokens * USD_PER_MTOK / 1e6 };
}
const money = (n) => n >= 100 ? '$' + Math.round(n) : '$' + n.toFixed(2);

// ---------- every tile, and where it stands ----------
// index answers, for a tile name: what it is, and what happened to it in
// each need of its capability. chosen | legal | out | off | idle.
function tileIndex(result, catalogue, off) {
  const by = {};
  const at = (name, cap) => by[name] || (by[name] = { name, cap, info: null, uses: [] });
  const info = Object.fromEntries(((catalogue && catalogue.tiles) || []).filter(t => t.pass === 'select').map(t => [t.name, t]));
  const caps = ((catalogue && catalogue.capabilities) || []).slice().sort((a, b) => CAP_ORDER.indexOf(a.name) - CAP_ORDER.indexOf(b.name));
  for (const cap of caps) for (const name of cap.offered_by || []) if (CAP_LABEL[cap.name] && !by[name]) at(name, cap.name).info = info[name] || null;
  for (const c of result ? result.explain.coverage : []) {
    for (const x of c.candidates) at(x.tile, c.capability).uses.push({ c, cand: x, status: x.tile === c.chosen ? 'chosen' : 'legal', behind: x.score - c.score });
    for (const x of c.illegal || []) at(x.tile, c.capability).uses.push({ c, status: 'out', reason: x.reason });
  }
  for (const t of Object.values(by)) {
    const has = (st) => t.uses.some(u => u.status === st);
    t.status = off[t.name] ? 'off' : has('chosen') ? 'chosen' : has('legal') ? 'legal' : has('out') ? 'out' : 'idle';
  }
  return by;
}
const STATUS_SAYS = { chosen: 'chosen', legal: 'can do it, costs more', out: 'ruled out', off: 'switched off', idle: 'not asked for' };
const CAP_ORDER = ['api', 'auth', 'event-bus', 'store', 'secrets', 'transport'];

function Wall({ index, inspect }) {
  const caps = CAP_ORDER.filter(cap => Object.values(index).some(t => t.cap === cap));
  const rank = { chosen: 0, legal: 1, off: 2, out: 3, idle: 4 };
  return html`<div className="wall">
    <div className="legend"><span><${Tile} g="g-app" />chosen</span><span><${Tile} g="g-app" kind="legal" />can do it, costs more</span><span><${Tile} kind="out" />ruled out or switched off</span></div>
    ${caps.map(cap => {
      const tiles = Object.values(index).filter(t => t.cap === cap).sort((a, b) => rank[a.status] - rank[b.status] || a.name.localeCompare(b.name));
      const asked = tiles.some(t => t.status !== 'idle' && t.status !== 'off');
      return html`<section key=${cap} className=${`shelf g-${cap} ${asked ? '' : 'unasked'}`}>
        <h3>${CAP_LABEL[cap]}<small>${asked ? `${tiles.filter(t => t.status === 'chosen' || t.status === 'legal').length} of ${tiles.length} can do what you asked` : 'not part of this design'}</small></h3>
        <div className="slabs">${tiles.map(t => {
          const use = t.uses.find(u => u.status === 'chosen') || t.uses.find(u => u.status === 'legal');
          return html`<button key=${t.name} className=${`slab ${t.status}`} onClick=${() => inspect(t.name)} aria-label=${`${t.name}: ${STATUS_SAYS[t.status]}`}>
            <${Tile} g=${'g-' + cap} kind=${t.status === 'chosen' ? '' : t.status === 'legal' ? 'legal' : t.status === 'idle' ? 'legal' : 'out'} />
            <b>${t.name}</b>
            <span>${use ? `cost ${use.cand.score}` : STATUS_SAYS[t.status]}</span>
          </button>`;
        })}</div>
      </section>`;
    })}
    ${caps.length ? null : html`<p className="hint">The tile catalogue is not available here.</p>`}
  </div>`;
}

// ---------- what is behind one tile ----------
function Inspector({ name, index, result, error, off, setOff, running, weights, go, back, canBack, close, openFile }) {
  const t = index[name];
  const ref = useRef(null);
  useEffect(() => { if (ref.current) { ref.current.scrollTop = 0; ref.current.focus(); } }, [name]);
  if (!t) return null;
  const info = t.info || {}, glaze = 'g-' + t.cap;
  const used = t.uses.find(u => u.status === 'chosen') || t.uses.find(u => u.status === 'legal');
  const terms = used ? used.cand.cost || [] : DIMS.map(([d]) => ({ dimension: d, value: (info.cost || {})[d] || 0, weight: weights[d], note: (info.cost_notes || {})[d] || '' }));
  const chain = used ? used.cand.chain || [] : [];
  const total = used ? used.cand.score : terms.reduce((n, x) => n + x.value * x.weight, 0);
  const most = Math.max(...terms.map(x => x.value * x.weight), 1);
  const asked = new Set(t.uses.flatMap(u => u.c.requirements || []));
  const facts = result ? fileFacts(result) : { by: {}, tasks: {} };
  const files = Object.keys(facts.by).filter(p => facts.by[p] === name).sort();
  const sibs = Object.values(index).filter(x => x.cap === t.cap && x.name !== name).sort((a, b) => a.name.localeCompare(b.name));
  const alias = (info.covers || []).map(c => (c.match(/^\(alias (.+)\)$/) || [])[1]).filter(Boolean);
  const list = (obj) => Object.keys(obj || {}).sort();
  return html`<div className="scrim" onClick=${close}>
    <aside ref=${ref} tabIndex="-1" className=${`drawer ${glaze}`} role="dialog" aria-modal="true" aria-label=${`tile ${name}`} onClick=${e => e.stopPropagation()}>
      <div className="dh">
        <button className="btn ghost small" disabled=${!canBack} onClick=${back} aria-label="back">← Back</button>
        <span className="crumb">${CAP_LABEL[t.cap]} <i>›</i> ${name}</span>
        <button className="btn ghost small" onClick=${close} aria-label="close">Close</button>
      </div>
      <div className="hero">
        <${Tile} g=${glaze} kind=${t.status === 'chosen' ? '' : t.status === 'legal' || t.status === 'idle' ? 'legal' : 'out'} />
        <div><h3>${name}</h3><span className=${`pill ${t.status}`}>${STATUS_SAYS[t.status]}</span>${used && used.cand.draft ? html`<span className="pill">draft</span>` : null}</div>
        <div className="score">${total}<small>cost</small></div>
      </div>
      ${info.doc ? html`<p className="doc">${info.doc[0].toUpperCase() + info.doc.slice(1)}.</p>` : null}
      <label className="allow" htmlFor="tile-allow">
        <${Switch} id="tile-allow" on=${!off[name]} glaze=${glaze} label=${`tilegen may choose ${name}`} set=${v => setOff(o => ({ ...o, [name]: !v }))} />
        <span><b>tilegen may choose this tile</b><small>${off[name] ? 'Switched off: the policy now says (avoid ' + name + ').' : 'Switch it off and tilegen decides again without it.'}</small></span>
      </label>

      ${error ? html`<div className="note">With the current ticks and switches nothing can cover every need, so what follows is the last design that worked.</div>` : null}
      ${t.uses.length ? html`<section><h4>${error ? 'In the last design' : 'In this design'}</h4>
        ${t.uses.map(u => html`<div key=${u.c.need} className=${`use ${u.status}`}>
          <b>${u.c.need.split('.').pop()}</b>
          ${u.status === 'chosen' ? html`<span>Chosen at cost ${u.cand.score}${u.c.candidates.length > 1 ? `, ${Math.min(...u.c.candidates.filter(x => x.tile !== name).map(x => x.score)) - u.cand.score} cheaper than the next` : ', the only tile that can do it'}.</span>`
            : u.status === 'legal' ? html`<span>Can do it at cost ${u.cand.score}, ${u.behind} more than <button className="tlink" onClick=${() => go(u.c.chosen)}>${u.c.chosen}</button>.</span>`
            : html`<span>${u.reason[0].toUpperCase() + u.reason.slice(1)}.</span>`}
        </div>`)}</section>` : html`<section><h4>In this design</h4><p className="hint">Nothing in the spec asks for ${CAP_LABEL[t.cap].toLowerCase()}, so this tile is not in play.</p></section>`}

      <section><h4>What it costs<small>value × your weight</small></h4>
        <div className="costs">${terms.map(x => html`<${React.Fragment} key=${x.dimension}>
          <span className="dim"><i style=${{ background: `var(${DIM_VAR[x.dimension] || '--d-dep'})` }}></i>${(DIMS.find(d => d[0] === x.dimension) || [0, x.dimension])[1]}</span>
          <span className="meter"><i style=${{ width: `${(x.value * x.weight / most) * 100}%`, background: `var(${DIM_VAR[x.dimension] || '--d-dep'})` }}></i></span>
          <span className="num">${x.value} × ${x.weight} = <b>${x.value * x.weight}</b></span>
          ${x.note ? html`<span className="note2">${x.note}</span>` : null}
        </${React.Fragment}>`)}
        ${chain.map(x => html`<${React.Fragment} key=${x.rule}>
          <span className="dim"><i style=${{ background: 'var(--d-llm)' }}></i>${x.rule}</span><span className="meter"></span><span className="num">+ <b>${x.score}</b></span>
          <span className="note2">What comes with it: the ${x.rule} tile${x.detail ? ', for ' + x.detail : ''}.</span>
        </${React.Fragment}>`)}</div></section>

      ${list(info.satisfies).length ? html`<section><h4>What it guarantees</h4>
        ${list(info.satisfies).map(r => html`<div key=${r} className=${`fact ${asked.has(r) ? 'asked' : ''}`}><code>${r}</code><span>${info.satisfies[r]}${asked.has(r) ? html` <em>you asked for this</em>` : null}</span></div>`)}</section>` : null}
      ${list(info.illegal_when).length ? html`<section><h4>Ruled out when you ask for</h4>
        ${list(info.illegal_when).map(r => html`<div key=${r} className=${`fact no ${asked.has(r) ? 'asked' : ''}`}><code>${r}</code><span>${info.illegal_when[r]}</span></div>`)}</section>` : null}
      ${list(info.unverified).length ? html`<section><h4>Claimed, never proven</h4>
        ${list(info.unverified).map(r => html`<div key=${r} className="fact"><code>${r}</code><span>${info.unverified[r]}</span></div>`)}</section>` : null}

      ${engineOf(info) || (info.requires || []).length || info.module || alias.length ? html`<section><h4>What it needs</h4>
        ${engineOf(info) ? html`<div className="fact"><code>${info.engine}</code><span>${running[info.engine] ? 'An engine your team already runs, so it costs nothing more to operate.' : 'An engine that has to be running.'}</span></div>` : null}
        ${(info.requires || []).map(r => html`<div key=${r} className="fact"><code>${r}</code><span>A tool on your machine at build time.</span></div>`)}
        ${info.module ? html`<div className="fact"><code>${info.module}</code><span>The Go module it uses.</span></div>` : null}
        ${alias.length ? html`<div className="fact"><code>${alias.join(', ')}</code><span>Also answers to this name in a config.</span></div>` : null}
      </section>` : null}

      ${files.length ? html`<section><h4>What it wrote</h4>
        <div className="wrote">${files.map(p => html`<button key=${p} className="holerow" onClick=${() => { openFile(p); close(); }}><b>${p}</b>
          <span>${(facts.tasks[p] || []).length ? `${facts.tasks[p].length} hole${facts.tasks[p].length > 1 ? 's' : ''} left open` : 'complete'}</span></button>`)}</div></section>` : null}

      ${sibs.length ? html`<section><h4>Other ${CAP_LABEL[t.cap].toLowerCase()} tiles</h4>
        <div className="sibs">${sibs.map(x => html`<button key=${x.name} className=${`slab mini ${x.status}`} onClick=${() => go(x.name)}>
          <${Tile} g=${glaze} kind=${x.status === 'chosen' ? '' : x.status === 'legal' || x.status === 'idle' ? 'legal' : 'out'} /><b>${x.name}</b><span>${STATUS_SAYS[x.status]}</span></button>`)}</div></section>` : null}
    </aside>
  </div>`;
}

// ---------- the design as a diagram ----------
// The picture is drawn from the decisions: one box per need with the tile
// that covers it, and a cylinder per engine that has to be running.
const TRANSPORT_SAYS = { 'http-json': 'HTTP and JSON', connect: 'Connect', grpc: 'gRPC' };
const engineOf = (info) => info && info.engine && info.engine !== 'none' ? info.engine : '';
function diagram(result, index, running, dir) {
  const cov = result ? result.explain.coverage : [];
  const out = ['flowchart ' + (dir || 'LR')], ids = {}, engines = {}, edges = [], pkgs = {};
  const q = (text) => '"' + String(text).replace(/"/g, "'") + '"';
  cov.forEach((c, i) => { ids[c.need] = 'n' + i; (pkgs[c.need.split('.')[0]] = pkgs[c.need.split('.')[0]] || []).push(c); });
  const pkgNames = Object.keys(pkgs).sort();
  const anyAPI = cov.some(c => c.capability === 'api');
  if (anyAPI) out.push('  clients(["Clients"]):::edge');
  pkgNames.forEach((pkg, pi) => {
    const of = (cap) => pkgs[pkg].filter(c => c.capability === cap);
    out.push(`  subgraph p${pi}[${q('package ' + pkg)}]`);
    for (const c of pkgs[pkg]) out.push(`    ${ids[c.need]}[${q(`<b>${c.need.split('.').pop()}</b><br/>${c.chosen}`)}]:::${c.capability.replace('-', '')}`);
    out.push('  end');
    const has = (c, r) => (c.requirements || []).includes(r);
    for (const api of of('api')) {
      edges.push(`  clients -->|${q(TRANSPORT_SAYS[api.chosen] || api.chosen)}| ${ids[api.need]}`);
      if (has(api, 'streaming')) edges.push(`  ${ids[api.need]} -.->|${q('live events')}| clients`);
      for (const a of of('auth')) edges.push(`  ${ids[api.need]} -->|${q('checks the caller')}| ${ids[a.need]}`);
      for (const s of of('store')) edges.push(`  ${ids[api.need]} -->|${q('reads and writes')}| ${ids[s.need]}`);
      for (const b of of('event-bus')) if (!of('store').some(s => has(s, 'outbox'))) edges.push(`  ${ids[api.need]} -->|${q('publishes')}| ${ids[b.need]}`);
    }
    for (const s of of('store')) for (const b of of('event-bus')) {
      if (has(s, 'outbox')) edges.push(`  ${ids[s.need]} -->|${q('outbox relay')}| ${ids[b.need]}`);
      if (has(s, 'inbox')) edges.push(`  ${ids[b.need]} -->|${q('inbox: once per event')}| ${ids[s.need]}`);
    }
  });
  for (const c of cov) {
    const e = engineOf(index[c.chosen] && index[c.chosen].info);
    if (!e) continue;
    if (!engines[e]) { engines[e] = 'e' + Object.keys(engines).length; out.push(`  ${engines[e]}[(${q(`<b>${e}</b><br/>${running[e] ? 'already running' : 'to operate'}`)})]:::engine`); }
    edges.push(`  ${ids[c.need]} --> ${engines[e]}`);
  }
  return { text: out.concat(edges).join('\n'), nodes: Object.fromEntries(cov.map(c => [ids[c.need], c.chosen])) };
}

// Mermaid is large, so it is fetched the first time the diagram is shown.
let mermaidLoading = null;
const loadMermaid = () => mermaidLoading || (mermaidLoading = new Promise((resolve, reject) => {
  if (window.mermaid) return resolve();
  const el = document.createElement('script');
  el.src = window.TILEGEN_MERMAID || 'mermaid.js';
  el.onload = () => window.mermaid ? resolve() : reject(new Error('not loaded'));
  el.onerror = () => { mermaidLoading = null; reject(new Error('not loaded')); };
  document.head.appendChild(el);
}));

function Drawing({ d, inspect }) {
  const [svg, setSvg] = useState('');
  const [failed, setFailed] = useState('');
  const box = useRef(null);
  useEffect(() => {
    let live = true;
    loadMermaid().then(() => {
      if (!live) return;
      const css = getComputedStyle(document.documentElement), v = (n) => css.getPropertyValue(n).trim();
      const paint = (cap, g) => `classDef ${cap} fill:${v('--raised')},stroke:${v(g)},stroke-width:2px,color:${v('--ink')}`;
      const text = d.text + '\n  ' + [paint('api', '--g-api'), paint('auth', '--g-auth'), paint('eventbus', '--g-events'), paint('store', '--g-store'), paint('secrets', '--g-secrets'), paint('transport', '--g-app'),
        `classDef engine fill:${v('--wash')},stroke:${v('--muted')},color:${v('--ink')}`, `classDef edge fill:${v('--ink')},stroke:${v('--ink')},color:${v('--bg')}`].join('\n  ');
      window.mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'base', fontFamily: v('--font-body'),
        themeVariables: { primaryColor: v('--raised'), primaryTextColor: v('--ink'), primaryBorderColor: v('--line'), lineColor: v('--muted'), clusterBkg: v('--surface'), clusterBorder: v('--line'), edgeLabelBackground: v('--bg'), fontSize: '15px' },
        flowchart: { curve: 'basis', padding: 14, htmlLabels: true, useMaxWidth: true } });
      return window.mermaid.render('blueprint-' + (++drawn), text).then(r => { if (live) { setSvg(r.svg); setFailed(''); } });
    }).catch(e => { if (live) setFailed(window.mermaid ? 'The diagram could not be drawn: ' + (e && e.message || e) : 'The diagram library did not load here, so this is the diagram as text.'); });
    return () => { live = false; };
  }, [d.text]);
  useEffect(() => {
    if (!box.current) return;
    for (const g of box.current.querySelectorAll('g.node')) {
      const key = Object.keys(d.nodes).find(k => new RegExp('(^|-)' + k + '(-|$)').test(g.id));
      if (!key) continue;
      g.style.cursor = 'pointer'; g.setAttribute('tabindex', '0'); g.setAttribute('role', 'button'); g.setAttribute('aria-label', 'tile ' + d.nodes[key]);
      g.onclick = () => inspect(d.nodes[key]);
      g.onkeydown = (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); inspect(d.nodes[key]); } };
    }
  }, [svg]);
  return html`<${React.Fragment}>
    ${failed ? html`<div className="note">${failed}</div>` : null}
    ${svg && !failed ? html`<div className="sheet" ref=${box} dangerouslySetInnerHTML=${{ __html: svg }}></div>` : failed ? html`<pre className="code plain">${d.text}</pre>` : html`<div className="sheet"><p className="hint">Drawing…</p></div>`}
  </${React.Fragment}>`;
}
let drawn = 0;

function Blueprint({ result, index, running, inspect }) {
  const tall = useMemo(() => diagram(result, index, running, 'TB'), [result, index, running]);
  const wide = useMemo(() => diagram(result, index, running, 'LR'), [result, index, running]);
  const [big, setBig] = useState(false);
  const [said, setSaid] = useState('');
  useEffect(() => {
    if (!big) return;
    const key = (e) => { if (e.key === 'Escape') setBig(false); };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [big]);
  if (!result || !result.explain.coverage.length) return html`<p className="hint">Nothing to draw yet.</p>`;
  return html`<div className="blueprint">
    <p className="hint">Drawn from the decisions: each box is a need and the tile covering it, each cylinder an engine that has to be running. Select a box to see what is behind it.</p>
    <${Drawing} d=${tall} inspect=${inspect} />
    <div className="row"><button className="btn ghost small" onClick=${() => setBig(true)}>Full screen</button>
      <button className="btn ghost small" onClick=${async () => setSaid(await copyText(wide.text) ? 'Copied' : 'Select the text to copy it')}>Copy as Mermaid</button>${said ? html`<span className="hint" role="status">${said}</span>` : null}</div>
    ${big ? html`<div className="scrim top wideview" onClick=${() => setBig(false)}>
      <div className="blueprint bigsheet" role="dialog" aria-modal="true" aria-label="architecture" onClick=${e => e.stopPropagation()}>
        <div className="row"><b>Architecture</b><span style=${{ flex: 1 }}></span><button className="btn ghost small" onClick=${() => setBig(false)}>Close</button></div>
        <${Drawing} d=${wide} inspect=${(n) => { setBig(false); inspect(n); }} />
      </div></div>` : null}
  </div>`;
}

function Decisions({ result, error, note, view, setView, index, inspect, running, off, setOff }) {
  const cov = result ? result.explain.coverage : [];
  const files = result ? Object.keys(result.files).length : 0;
  const total = cov.reduce((n, c) => n + c.score, 0);
  const saved = useMemo(() => saving(result), [result]);
  const views = [['decisions', 'Decisions', cov.length], ['tiles', 'Tiles', Object.keys(index).length], ['map', 'Architecture', null]];
  return html`<section id="decide" className="pane decisions">
    <header><span className="step">2</span><h2>What tilegen chose, and why</h2>
      <p>Each need goes to the cheapest tile that can do everything asked of it. The rest are shown with the reason they lost.</p></header>
    ${error ? html`<${Refusal} error=${error} off=${off} setOff=${setOff} />` : null}
    ${note ? html`<div className="note">${note}</div>` : null}
    ${result && (result.drafted || []).length ? html`<div className="callout draftwin">
      <h3>A draft won, so there is no project yet</h3>
      <ul>${result.drafted.map(d => html`<li key=${d}><b>${d.split(': ')[1]}</b> would cover ${d.split(': ')[0].split('.').pop()}</li>`)}</ul>
      <p className="hint" style=${{ color: 'inherit' }}>The decision and its reasons below are real. A draft says what a tool is for and what it costs; it has no generator, so tilegen cannot write code for it until that tile is built in. Untick the draft to get the project from the tiles tilegen has.</p>
    </div>` : null}
    <div className=${error ? 'stale' : ''} style=${{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div className="summary">
        <span><b>${cov.length}</b>decision${cov.length === 1 ? '' : 's'}</span>
        <span><b>${total}</b>total cost</span>
        <span><b>${files}</b>files</span>
        <span><b>${result ? result.holes : 0}</b>holes left open</span>
      </div>
      ${saved.tokens ? html`<div className="saved" title=${`${saved.tokens.toLocaleString()} tokens, estimated at four characters each, at $${USD_PER_MTOK} per million output tokens (Claude Sonnet 5.5)`}>
        <strong>${money(saved.usd)} saved</strong>
        <span>every run: about <b>${saved.tokens.toLocaleString()}</b> tokens of code written by the compiler, not bought from an LLM at $${USD_PER_MTOK} per million.</span>
      </div>` : null}
      <div className="tabs" role="tablist">${views.map(([id, label, n]) => html`<button key=${id} role="tab" className="tab" aria-selected=${view === id} onClick=${() => setView(id)}>${label}${n ? html`<small>${n}</small>` : null}</button>`)}</div>
      ${view === 'tiles' ? html`<${Wall} index=${index} inspect=${inspect} />`
        : view === 'map' ? html`<${Blueprint} result=${result} index=${index} running=${running} inspect=${inspect} />`
        : html`<${React.Fragment}>
          <div className="legend" aria-label="what the cost bars are made of">${DIMS.map(([k, label, v]) => html`<span key=${k}><i style=${{ background: `var(${v})` }}></i>${label}</span>`)}</div>
          ${cov.map(c => html`<${Need} key=${c.need} c=${c} inspect=${inspect} />`)}
        </${React.Fragment}>`}
    </div>
  </section>`;
}

// ---------- find anything: ⌘K ----------
function Palette({ items, close }) {
  const [q, setQ] = useState('');
  const [at, setAt] = useState(0);
  const listRef = useRef(null);
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const found = useMemo(() => {
    const hit = items.filter(it => (it.always || words.length) && (!it.needsQuery || q.trim().length > 1) && words.every(w => it.hay.includes(w)));
    const score = (it) => (words.length && it.label.toLowerCase().startsWith(words[0]) ? 0 : 1);
    return hit.sort((a, b) => a.order - b.order || score(a) - score(b)).slice(0, 60);
  }, [q, items]);
  useEffect(() => { setAt(0); }, [q]);
  useEffect(() => { const el = listRef.current && listRef.current.querySelector('[aria-selected="true"]'); if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest' }); }, [at, found]);
  const run = (it) => { if (!it) return; close(); it.run(); };
  const key = (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setAt(i => Math.min(found.length - 1, i + 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setAt(i => Math.max(0, i - 1)); }
    else if (e.key === 'Enter') { e.preventDefault(); run(found[at]); }
    else if (e.key === 'Escape') { e.preventDefault(); close(); }
  };
  let last = '';
  return html`<div className="scrim top" onClick=${close}>
    <div className="palette" role="dialog" aria-modal="true" aria-label="search" onClick=${e => e.stopPropagation()}>
      <input id="palette-q" autoFocus className="pq" value=${q} placeholder="Search tiles, requirements, files, the spec and the policy" aria-label="search"
        role="combobox" aria-expanded="true" aria-controls="palette-list" onChange=${e => setQ(e.target.value)} onKeyDown=${key} />
      <div id="palette-list" className="plist" role="listbox" ref=${listRef}>
        ${found.length === 0 ? html`<p className="hint pad">Nothing matches “${q}”.</p>` : null}
        ${found.map((it, i) => {
          const head = it.group !== last ? html`<div key=${'g' + it.group} className="pgroup">${it.group}</div>` : null;
          last = it.group;
          return html`<${React.Fragment} key=${it.id}>${head}<button role="option" aria-selected=${i === at} className="pitem" onMouseMove=${() => setAt(i)} onClick=${() => run(it)}>
            ${it.glaze !== undefined ? html`<${Tile} g=${it.glaze} kind=${it.kind || ''} />` : html`<span className="pdot"></span>`}
            <span className="pl">${it.label}</span><span className="ps">${it.sub || ''}</span></button></${React.Fragment}>`;
        })}
      </div>
      <div className="pfoot"><span><kbd>↑</kbd><kbd>↓</kbd> move</span><span><kbd>↵</kbd> open</span><span><kbd>esc</kbd> close</span></div>
    </div>
  </div>`;
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
      : !Object.keys(result.files).length && tab !== 'spec' ? html`<p className="note">No files: a draft tile was chosen, and a draft cannot generate code. The middle pane says which.</p>`
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
  const [off, setOff] = useState(boot.off || {}); // tiles switched off by hand
  const policy = useMemo(() => TG.buildPolicy(state, running, off), [state, running, off]);
  const [view, setView] = useState(boot.view || 'decisions');
  const [catalogue, setCatalogue] = useState(null);
  const [trail, setTrail] = useState([]); // tiles opened in the inspector, newest last
  const [finding, setFinding] = useState(false);
  const inspect = useCallback((name) => setTrail(t => t[t.length - 1] === name ? t : t.concat(name)), []);
  const readGoMods = useCallback(async (files) => {
    if (!compile.current) return;
    const r = await compile.current.gomods(files);
    setScan(prev => ({ ...r, asked: files.length || (prev && prev.asked) || 0, files: files.length ? r : prev }));
    if (files.length) setRunning(Object.fromEntries((r.running || []).map(x => [x.engine, true])));
  }, []);
  useEffect(() => { if (engine.status === 'live') readGoMods([]).catch(() => {}); }, [engine.status]);
  // Drafts last for this visit only: tiles declared here, by hand or by
  // Claude, that take part in the decisions and generate nothing.
  const [drafts, setDrafts] = useState([]); // { id, module, text, on, note }
  const tiles = useMemo(() => drafts.filter(d => d.on && d.text.trim()).map(d => d.text.trim()).join('\n\n'), [drafts]);
  const askClaude = useCallback(async (modules) => {
    const r = await compile.current.draft(modules);
    if (r.error) throw new Error(r.error);
    setDrafts(prev => {
      const keep = prev.filter(d => !modules.some(m => m.module === d.module));
      return keep.concat(r.drafts.map((d, i) => ({ id: Date.now() + '-' + i, module: d.module, text: d.tile || '', on: false, note: d.not_tile ? 'Not a tile: ' + d.not_tile : (d.error || d.from_memory || (d.symbols ? 'Written from ' + d.symbols + ' symbols the Go type checker read from the module, and checked against them.' : '')) })));
    });
    return r;
  }, []);
  const spec = custom === null ? built : custom;

  useEffect(() => {
    let live = true;
    serverEngine().then(fn => ({ fn, where: 'server' })).catch(() => loadEngine().then(fn => ({ fn, where: 'page' })))
      .then(({ fn, where }) => {
        if (!live) return;
        compile.current = fn; setEngine({ status: 'live', where, version: window.tilegenVersion || '' });
        fn.tiles().then(c => { if (live && c && c.tiles) setCatalogue(c); }).catch(() => {});
      })
      .catch(() => { if (live) setEngine({ status: 'off' }); });
    return () => { live = false; };
  }, []);

  useEffect(() => {
    if (engine.status !== 'live') return;
    setBusy(true);
    const t = setTimeout(async () => {
      let r, added = false;
      try {
        r = await compile.current.compile(spec, policy, tiles);
        if (r.error && custom === null && TG.NEEDS_SESSION.test(r.error)) { // tilegen says what the chosen tile needs; give it that
          r = await compile.current.compile(TG.buildSpec(state, { session: true }), policy, tiles);
          added = true;
        }
      } catch (e) { r = { error: String(e && e.message || e) }; }
      setBusy(false);
      if (r.error) { setError(r.error.replace(/^(spec|config|policy)\.sexp:\d+:\d+: /, '')); return; }
      setError(''); setResult({ ...r, files: r.files || {} });
      setNote(added ? 'Signing in this way keeps sessions in a store, so a Session entity was added to the spec. tilegen asked for it by name.' : '');
    }, 140);
    return () => clearTimeout(t);
  }, [spec, policy, tiles, engine.status]);

  useEffect(() => {
    const hot = window.claude && window.claude.hot;
    if (hot && hot.snapshot) hot.snapshot(() => ({ state, presetId, custom, tab, open, running, off, view }));
  });

  const index = useMemo(() => tileIndex(result, catalogue, off), [result, catalogue, off]);
  useEffect(() => {
    const key = (e) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') { e.preventDefault(); setFinding(f => !f); }
      else if (e.key === 'Escape') { setFinding(false); setTrail([]); }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, []);
  const openFile = (p) => { setOpen(p); setTab('files'); const el = document.getElementById('code'); if (el && el.scrollIntoView) el.scrollIntoView({ block: 'start' }); };
  const showView = (v) => { setView(v); const el = document.getElementById('decide'); if (el && el.scrollIntoView) el.scrollIntoView({ block: 'start' }); };
  const items = useMemo(() => {
    if (!finding) return [];
    const out = [];
    const add = (group, order, id, label, sub, run, more) => out.push({ group, order, id: group + ':' + id, label, sub, run, hay: (label + ' ' + (sub || '') + ' ' + group + ' ' + ((more && more.words) || '')).toLowerCase(), ...(more || {}) });
    for (const t of Object.values(index).sort((a, b) => a.name.localeCompare(b.name))) {
      add('Tiles', 1, t.name, t.name, `${CAP_LABEL[t.cap]} · ${STATUS_SAYS[t.status]}`, () => inspect(t.name),
        { glaze: 'g-' + t.cap, kind: t.status === 'chosen' ? '' : t.status === 'out' || t.status === 'off' ? 'out' : 'legal', always: true, words: (t.info && t.info.doc) || '' });
    }
    for (const area of Object.keys(TG.SHEET)) for (const r of TG.SHEET[area]) {
      const on = state[area].on && !!state[area].reqs[r.req];
      add('Requirements', 2, area + r.req, r.say, `${AREAS[area].title} · ${r.req} · ${on ? 'ticked' : 'not ticked'}`, () => {
        setCustom(null); setPresetId(null);
        setState(s => { const n = JSON.parse(JSON.stringify(s)); if (on) n[area].reqs[r.req] = false; else { n[area].on = true; n[area].reqs[r.req] = true; } return n; });
      }, { glaze: AREAS[area].glaze, kind: on ? '' : 'legal', always: true });
    }
    for (const [area, key, say] of [['events', 'outbox', 'Saved and published, or neither'], ['events', 'inbox', 'Delivered twice, handled once'], ['api', 'watch', 'Clients watch events as a live stream']]) {
      const on = !!state[area][key];
      add('Requirements', 2, area + key, say, `${AREAS[area].title} · ${key} · ${on ? 'ticked' : 'not ticked'}`, () => {
        setCustom(null); setPresetId(null);
        setState(s => on ? { ...s, [area]: { ...s[area], [key]: false } } : TG.apply(s, [{ area: 'events', what: 'on' }, { area, what: 'on' }, { area, what: key }]));
      }, { glaze: AREAS[area].glaze, kind: on ? '' : 'legal', always: true });
    }
    for (const p of TG.PRESETS) add('Briefs', 3, p.id, p.title, p.brief, () => { setState(JSON.parse(JSON.stringify(p.state))); setPresetId(p.id); setCustom(null); }, { always: true });
    add('Go to', 4, 'decisions', 'Decisions', 'why each tile won', () => showView('decisions'), { always: true });
    add('Go to', 4, 'tiles', 'Tiles', 'every tile and where it stands', () => showView('tiles'), { always: true });
    add('Go to', 4, 'map', 'Architecture', 'the design as a diagram', () => showView('map'), { always: true });
    add('Go to', 4, 'spec', 'Spec and policy', 'what tilegen was given', () => { openFile(open); setTab('spec'); }, { always: true });
    add('Go to', 4, 'build', 'Build it', 'download and next steps', () => { openFile(open); setTab('build'); }, { always: true });
    add('Go to', 4, 'clear', 'Clear all', 'untick every requirement', () => { setState(s => TG.clear(s)); setPresetId(null); setOff({}); }, { always: true });
    for (const p of Object.keys((result && result.files) || {}).sort()) add('Files', 5, p, p.split('/').pop(), p, () => openFile(p));
    const lines = (text, where) => text.split('\n').forEach((l, i) => { if (l.trim() && !/^\s*;/.test(l)) add(where, 6, where + i, l.trim(), `${where.toLowerCase()} · line ${i + 1}`, () => { openFile(open); setTab('spec'); }, { needsQuery: true }); });
    lines(spec, 'Spec'); lines(policy, 'Policy');
    return out;
  }, [finding, index, state, result, spec, policy, open]);

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
      <button className="chip find" onClick=${() => setFinding(true)} aria-label="search">Search <kbd>${/Mac|iPhone|iPad/.test(navigator.platform || '') ? '⌘K' : 'Ctrl K'}</kbd></button>
      ${engine.where === 'server' ? html`<a className="chip" href="docs">Docs</a>` : null}
      <span className=${`engine ${engine.status} ${busy ? 'busy' : ''}`} role="status"><i></i>${pill}</span>
    </header>
    <main className="bench">
      <${Sheet} state=${state} setState=${setState} presetId=${presetId} setPresetId=${setPresetId} off=${off} setOff=${setOff} paused=${custom !== null} resume=${() => setCustom(null)}
        scan=${scan} running=${running} setRunning=${setRunning} readGoMods=${readGoMods} live=${engine.status === 'live'}
        llm=${(compile.current && compile.current.llm) || { enabled: false, why: 'starting' }} drafts=${drafts} setDrafts=${setDrafts} askClaude=${askClaude} />
      <${Decisions} result=${result} error=${error} note=${note} view=${view} setView=${setView} index=${index} inspect=${inspect} running=${running} off=${off} setOff=${setOff} />
      <${Output} result=${result} spec=${built} policy=${policy} custom=${custom} setCustom=${setCustom} tab=${tab} setTab=${setTab} open=${open} setOpen=${setOpen} />
    </main>
    ${trail.length ? html`<${Inspector} name=${trail[trail.length - 1]} index=${index} result=${result} error=${error} off=${off} setOff=${setOff} running=${running} weights=${state.weights}
      go=${inspect} back=${() => setTrail(t => t.slice(0, -1))} canBack=${trail.length > 1} close=${() => setTrail([])} openFile=${openFile} />` : null}
    ${finding ? html`<${Palette} items=${items} close=${() => setFinding(false)} />` : null}
    <nav className="dock" aria-label="sections">
      <a href="#require"><b>1</b>Require</a><a href="#decide"><b>2</b>Decide</a><a href="#code"><b>3</b>Get the code</a>
    </nav>
  </${React.Fragment}>`;
}

const start = (boot) => ReactDOM.createRoot(document.getElementById('root')).render(html`<${App} boot=${boot || {}} />`);
if (window.claude && window.claude.hot && window.claude.hot.ready) window.claude.hot.ready(start); else start((window.claude && window.claude.hot && window.claude.hot.data) || {});
