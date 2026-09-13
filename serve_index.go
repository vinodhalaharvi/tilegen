package main

import (
	"net/http"
	"strings"
)

// The pages the service serves: a spec editor, and the documentation.
// One file, no build step, no external request: the service stays a
// single binary and works offline.

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(strings.ReplaceAll(indexHTML, "__VERSION__", version)))
}

const exampleSpec = `(project bookmarks
  (module example.com/bookmarks)
  (go 1.22)
  (require (uuid github.com/google/uuid v1.6.0)))

(package links
  (enum Visibility private unlisted public)

  (entity Link
    (field ID uuid.UUID)
    (field URL string)
    (field Title string)
    (field Visibility Visibility)
    (field SavedAt time.Time)
    (store get list save delete
      (list-by Visibility)
      (durable)                   ; must survive a restart
      (constraint "Saving the same URL twice must not create two links.")))

  (http
    (route GET    "/links"      (list Link))
    (route GET    "/links/{id}" (get Link))
    (route POST   "/links"      (save Link))
    (route DELETE "/links/{id}" (delete Link)))

  (events
    (event LinkSaved (field LinkID uuid.UUID))))

(package sessions
  (entity Session                 ; no (durable): may live in memory
    (field ID string)
    (field UserEmail string)
    (store get save delete)))

; Change these four lines and the storage, the schema, the queries and
; the store implementation all change. Nothing above them moves.
(policy
  (prefer sqlite-sqlc
    (strength required)
    (source ops "ships as one binary; no database server")))
`

const indexHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tilegen — describe a Go service, get the project</title>
<style>
  :root {
    --bg:#0f1419; --fg:#d7dde5; --dim:#7c8899; --rule:#222c37; --panel:#151c24;
    --kw:#7aa2c8; --str:#98b978; --com:#5a6775; --lit:#c9a26d; --mark:#d98a63; --hi:#e8edf3;
    --pol:#a98ad4; --punc:#4a5563;
  }
  @media (prefers-color-scheme: light) {
    :root {
      --bg:#fdfdfc; --fg:#1d252e; --dim:#606d7e; --rule:#e0e4e9; --panel:#f4f6f8;
      --kw:#2d6a94; --str:#4c7038; --com:#8a95a3; --lit:#8a6320; --mark:#b2542c; --hi:#0b1219;
      --pol:#6b4aa0; --punc:#a8b0ba;
    }
  }
  * { box-sizing:border-box; }
  html { scroll-behavior:smooth; }
  @media (prefers-reduced-motion:reduce) { html { scroll-behavior:auto; } }
  /* One family, one scale: 12px labels, 13px body and code, 15px headings,
     22px for the one title. Nothing else. */
  body { margin:0; background:var(--bg); color:var(--fg); font-size:13px; line-height:1.7;
         font-family:Monaco, Menlo, ui-monospace, SFMono-Regular, Consolas, monospace; }
  a { color:var(--kw); }
  :focus-visible { outline:2px solid var(--kw); outline-offset:2px; }

  .top { display:flex; align-items:center; gap:18px; padding:12px 22px;
         border-bottom:1px solid var(--rule); flex-wrap:nowrap; }
  .wordmark { color:var(--hi); font-weight:700; letter-spacing:-.2px; white-space:nowrap; }
  .tag { color:var(--dim); font-size:13px; overflow:hidden; text-overflow:ellipsis;
         white-space:nowrap; min-width:0; }
  .top nav { display:flex; gap:2px; margin-left:auto; flex:none; }
  .top nav button { font:inherit; font-size:13px; padding:4px 14px; cursor:pointer;
        border:1px solid transparent; border-radius:4px; background:none; color:var(--dim); }
  .top nav button[aria-selected=true] { color:var(--hi); border-color:var(--rule); background:var(--panel); }
  .top a.src { color:var(--dim); font-size:13px; text-decoration:none; }

  #try { display:grid; grid-template-columns:1fr 1fr; height:calc(100vh - 98px); }
  #try > div { display:flex; flex-direction:column; min-width:0; }
  #try > div + div { border-left:1px solid var(--rule); }
  .bar { display:flex; gap:8px; align-items:center; padding:8px 14px;
         border-bottom:1px solid var(--rule); color:var(--dim); font-size:12px; }
  .bar strong { color:var(--fg); font-weight:600; }
  .bar .fill { margin-left:auto; }
  #spec, #out { flex:1; margin:0; padding:14px 16px; border:0; overflow:auto; background:none;
        color:var(--fg); font:inherit; line-height:1.65; tab-size:2; }
  #spec { resize:none; outline:none; }
  #out { white-space:pre-wrap; }
  @media (max-width:900px) {
    #try { grid-template-columns:1fr; height:auto; }
    #try > div + div { border-left:0; border-top:1px solid var(--rule); }
    #spec { min-height:44vh; } #out { min-height:26vh; }
  }
  @media (max-width:620px) {
    .top { gap:12px; padding:12px 14px; }
    .top .tag { display:none; }
    .top a.src { display:none; }
    #docs { padding:0 14px 70px; }
  }
  .btn { font:inherit; font-size:13px; padding:5px 12px; border:1px solid var(--rule);
         border-radius:4px; background:none; color:var(--fg); cursor:pointer; }
  .btn:hover { border-color:var(--kw); color:var(--kw); }
  .btn.go { background:var(--kw); border-color:var(--kw); color:var(--bg); font-weight:600; }

  #docs { padding:0 22px 90px; }

  /* The builder: plain controls, two columns, same furniture as #try. */
  #start { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr);
           height:calc(100vh - 98px); }
  #start > div { display:flex; flex-direction:column; min-width:0; }
  #start > div + div { border-left:1px solid var(--rule); }
  #start .form { overflow:auto; padding:16px 18px 40px; }
  #start fieldset { border:0; border-top:1px solid var(--rule); margin:0 0 4px; padding:12px 0 6px; }
  #start fieldset:first-child { border-top:0; padding-top:0; }
  #start legend { color:var(--dim); font-size:12px; text-transform:lowercase;
                  padding:0 0 6px; }
  #start .row { display:flex; align-items:center; gap:10px; margin:0 0 7px; }
  #start .row span { color:var(--dim); font-size:13px; width:62px; flex:none; }
  #start input[type=text], #start .row input { font:inherit; font-size:13px; flex:1; min-width:0;
        background:var(--panel); color:var(--fg); border:1px solid var(--rule);
        border-radius:4px; padding:5px 8px; outline:none; }
  #start .row input:focus { border-color:var(--kw); }
  #start .opts { display:flex; flex-wrap:wrap; gap:4px 16px; }
  #start .opts.col { flex-direction:column; gap:5px; }
  #start .opts.sub { margin-top:6px; padding-left:16px; }
  #start .opts label { font-size:13px; color:var(--fg); cursor:pointer;
                       display:flex; align-items:center; gap:6px; }
  #start .opts input { accent-color:var(--kw); margin:0; }
  #start .hint { color:var(--mark); font-size:12px; margin:8px 0 0; }
  #f-out { white-space:pre-wrap; }
  @media (max-width:900px) {
    #start { grid-template-columns:1fr; height:auto; }
    #start > div + div { border-left:0; border-top:1px solid var(--rule); }
    #f-out { min-height:30vh; }
  }
  .doc { max-width:74rem; margin:0 auto; }
  .lede { margin:40px 0 4px; max-width:46rem; }
  .lede h1 { font-size:22px; line-height:1.4; margin:0 0 14px; font-weight:700; color:var(--hi);
             letter-spacing:-.4px; }
  .lede p { color:var(--dim); margin:0 0 10px; font-size:13px; }
  .doc h2 { font-size:15px; font-weight:700; color:var(--hi); margin:52px 0 4px;
            padding-top:16px; border-top:1px solid var(--rule); }
  .doc h2 .n { color:var(--dim); font-weight:400; margin-right:10px; }
  .doc h3 { font-size:13px; font-weight:700; color:var(--hi); margin:30px 0 4px; }
  .doc p, .doc li { max-width:46rem; color:var(--fg); font-size:13px; }
  .doc p { margin:10px 0; }
  .doc ul { max-width:46rem; padding-left:20px; }
  .doc .dim { color:var(--dim); }

  code { font:inherit; color:var(--hi); }
  pre { margin:0; padding:13px 15px; background:var(--panel); border:1px solid var(--rule);
        border-radius:5px; overflow-x:auto; }
  pre code { display:block; line-height:1.65; color:var(--fg); white-space:pre; }
  p code, li code, td code { background:var(--panel); padding:1px 5px; border-radius:3px; }

  .pair { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:12px; margin:14px 0; }
  @media (max-width:900px) { .pair { grid-template-columns:1fr; } }
  .pair > figure, .single { margin:0; min-width:0; }
  .single { margin:14px 0; }
  figcaption { font-size:12px; color:var(--dim); margin:0 0 5px; }
  figcaption b { color:var(--mark); font-weight:600; }

  .steps { counter-reset:step; }
  .step { margin:22px 0; }
  .step > h4 { font-size:13px; font-weight:700; color:var(--hi); margin:0 0 6px; }
  .step > h4::before { counter-increment:step; content:counter(step) ". "; color:var(--dim); font-weight:400; }

  table { border-collapse:collapse; width:100%; max-width:52rem; margin:12px 0; font-size:13px; }
  th, td { text-align:left; padding:6px 14px 6px 0; border-bottom:1px solid var(--rule); vertical-align:top; }
  th { color:var(--dim); font-weight:600; font-size:12px; }
  .aside { border-left:2px solid var(--rule); padding:2px 0 2px 14px; color:var(--dim);
           max-width:46rem; margin:16px 0; font-size:13px; }
  .try { font:inherit; font-size:13px; padding:5px 12px; border:1px solid var(--rule);
         border-radius:4px; background:none; color:var(--dim); cursor:pointer; margin:4px 0 0; }
  .try:hover { border-color:var(--kw); color:var(--kw); }

  .c { color:var(--com); } .s { color:var(--str); } .k { color:var(--kw); }
  .l { color:var(--lit); } .h { color:var(--mark); font-weight:700; }
  /* What a form decides, not just that it is a form: requirements and policy
     are the lines a reader changes, so they do not wear the same colour as
     the types around them. */
  .r { color:var(--mark); } .p { color:var(--pol); } .o { color:var(--str); }
  .d { color:var(--punc); } .n0 { color:var(--hi); }
  .ok { color:var(--str); } .no { color:var(--punc); }
  .foot { border-top:1px solid var(--rule); margin-top:50px; padding:14px 22px;
          color:var(--dim); font-size:13px; }
</style>

<div class="top">
  <span class="wordmark">tilegen</span>
  <span class="tag">describe a Go service, get the project</span>
  <nav>
    <button id="tab-start" aria-selected="false">Start</button>
    <button id="tab-try" aria-selected="true">Try it</button>
    <button id="tab-docs" aria-selected="false">Docs</button>
  </nav>
  <a class="src" href="https://github.com/vinodhalaharvi/tilegen">source</a>
</div>

<main id="start" hidden>
  <div>
    <div class="bar"><strong>answer a few questions</strong></div>
    <div class="form">
      <fieldset>
        <legend>The project</legend>
        <label class="row"><span>name</span><input id="f-name" value="notes"></label>
        <label class="row"><span>module</span><input id="f-module" value="example.com/notes"></label>
      </fieldset>

      <fieldset>
        <legend>What you are storing</legend>
        <label class="row"><span>called</span><input id="f-entity" value="Note"></label>
        <div class="opts" id="f-fields">
          <label><input type="checkbox" value="Title string" checked> Title</label>
          <label><input type="checkbox" value="Body string" checked> Body</label>
          <label><input type="checkbox" value="Done bool"> Done</label>
          <label><input type="checkbox" value="CreatedAt time.Time" checked> CreatedAt</label>
          <label><input type="checkbox" value="OwnerID uuid.UUID"> OwnerID</label>
        </div>
        <div class="opts">
          <label><input type="radio" name="id" value="int64" checked> ID is a number</label>
          <label><input type="radio" name="id" value="uuid.UUID"> ID is a UUID</label>
        </div>
      </fieldset>

      <fieldset>
        <legend>What you can do with it</legend>
        <div class="opts" id="f-ops">
          <label><input type="checkbox" value="get" checked> get</label>
          <label><input type="checkbox" value="list" checked> list</label>
          <label><input type="checkbox" value="save" checked> save</label>
          <label><input type="checkbox" value="delete"> delete</label>
          <label><input type="checkbox" value="count"> count</label>
        </div>
      </fieldset>

      <fieldset>
        <legend>What it needs</legend>
        <div class="opts">
          <label><input type="checkbox" id="f-durable" checked> must survive a restart</label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Also generate</legend>
        <div class="opts">
          <label><input type="checkbox" id="f-http" checked> HTTP routes</label>
          <label><input type="checkbox" id="f-events"> events</label>
          <label><input type="checkbox" id="f-enum"> a status enum</label>
        </div>
        <div class="opts sub" id="f-events-sub" hidden>
          <label><input type="checkbox" id="f-xproc"> handlers live in other processes</label>
        </div>
      </fieldset>

      <fieldset>
        <legend>Where it is kept</legend>
        <div class="opts col" id="f-store">
          <label><input type="radio" name="store" value="" checked> let tilegen decide</label>
          <label><input type="radio" name="store" value="memory"> in memory</label>
          <label><input type="radio" name="store" value="sqlite-sqlc"> sqlite, one file</label>
          <label><input type="radio" name="store" value="postgres-sqlc"> postgres, queries generated</label>
          <label><input type="radio" name="store" value="postgres-pgx"> postgres, queries by hand</label>
        </div>
        <p class="hint" id="f-warn" hidden></p>
      </fieldset>

      <fieldset id="f-policy-set" hidden>
        <legend>How firm is that</legend>
        <div class="opts col">
          <label><input type="radio" name="firm" value="prefer" checked> we would prefer it</label>
          <label><input type="radio" name="firm" value="required"> it has to be this</label>
        </div>
        <div class="opts">
          <label><input type="radio" name="who" value="team" checked> team</label>
          <label><input type="radio" name="who" value="ops"> ops</label>
          <label><input type="radio" name="who" value="client"> client</label>
        </div>
        <label class="row"><span>because</span><input id="f-why" value="we run one postgres for everything"></label>
      </fieldset>
    </div>
  </div>
  <div>
    <div class="bar">
      <strong>your spec</strong><span class="fill"></span>
      <button class="btn go" id="f-open">Open in the editor</button>
    </div>
    <pre id="f-out"></pre>
  </div>
</main>

<main id="try">
  <div>
    <div class="bar">
      <strong>spec</strong><span class="fill"></span>
      <button class="btn" id="explain">What would it do?</button>
      <button class="btn go" id="download">Download project</button>
    </div>
    <textarea id="spec" spellcheck="false">` + exampleSpec + `</textarea>
  </div>
  <div>
    <div class="bar"><strong id="title">result</strong></div>
    <pre id="out">Edit the spec, then press What would it do? to see how each store
will be kept, or Download project for the whole thing as a zip.

First time here? Open Docs.</pre>
  </div>
</main>

<div id="docs" hidden><div class="doc">` + docsHTML + `</div></div>

<div class="foot">tilegen __VERSION__ · go install github.com/vinodhalaharvi/tilegen@latest</div>

<script>
const $ = id => document.getElementById(id);
const esc = s => s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");
const span = (cls, text) => '<span class="' + cls + '">' + esc(text) + "</span>";

/* One pass per language. Chaining regexes over already-marked-up text is
   how you end up highlighting your own span tags. */

// The vocabulary, grouped by what a form decides rather than by shape, so a
// reader can tell a requirement from a type without being told which is which.
const SX_REQ = new Set("durable cross-process".split(" "));
const SX_POLICY = new Set(("policy prefer avoid weights margin strength source " +
  "required strong weak llm-work maintenance dependency runtime uncertainty " +
  "client team ops derived measured user").split(" "));
const SX_OP = new Set(("get list save delete count list-by get-by count-by " +
  "exists-by delete-by").split(" "));
// Names of things you can choose, wherever they appear: a value, not a form.
const SX_NAME = new Set(("memory sqlite-sqlc postgres-sqlc postgres-pgx " +
  "local-bus nats-bus sqlite postgres pgx local nats auto").split(" "));

function paintSexp(src) {
  let out = "", i = 0;
  while (i < src.length) {
    const ch = src[i];
    if (ch === ";") { const j = src.indexOf("\n", i), k = j < 0 ? src.length : j;
      out += span("c", src.slice(i, k)); i = k; continue; }
    if (ch === '"') { let j = i + 1;
      while (j < src.length && (src[j] !== '"' || src[j-1] === "\\")) j++;
      out += span("s", src.slice(i, Math.min(j + 1, src.length))); i = j + 1; continue; }
    if (ch === "(" || ch === ")") { out += span("d", ch); i++; continue; }
    if (/[\w\-\/.*\[\]]/.test(ch)) {
      let j = i;
      while (j < src.length && /[\w\-\/.*\[\]]/.test(src[j])) j++;
      const w = src.slice(i, j);
      out += span(sexpClass(w), w); i = j; continue;
    }
    out += esc(ch); i++;
  }
  return out;
}

// A word's colour: what it decides first, then what it looks like.
function sexpClass(w) {
  if (SX_REQ.has(w)) return "r";
  if (SX_NAME.has(w)) return "n0";
  if (SX_POLICY.has(w)) return "p";
  if (SX_OP.has(w)) return "o";
  if (/^[0-9]/.test(w)) return "l";
  if (/^\*?[a-z][\w]*\.[A-Z]/.test(w) || /^\*?(string|bool|byte|rune|error|int|int8|int16|int32|int64|uint|uint8|uint16|uint32|uint64|float32|float64)$/.test(w))
    return "l";
  if (/^[a-z][\w\-]*$/.test(w)) return "k";
  return "n0";
}

// The answer panels, painted the way the live result panel already paints
// them, so a sample in the docs and a real answer read identically.
function paintAnswer(src) {
  return src.split("\n").map(line => {
    const kept = line.match(/^(\s*kept in\s+)(\S+)(.*)$/);
    if (kept) return span("c", esc(kept[1])) + span("ok", esc(kept[2])) + esc(kept[3]);
    const ruled = line.match(/^(\s*ruled out\s+)(\S+)(\s*)(.*)$/);
    if (ruled) return span("c", esc(ruled[1])) + span("no", esc(ruled[2])) +
      esc(ruled[3]) + span("c", esc(ruled[4]));
    const also = line.match(/^(\s*also possible\s+)(\S+)(.*)$/);
    if (also) return span("c", esc(also[1])) + esc(also[2]) + esc(also[3]);
    const asked = line.match(/^(\s*you asked for\s+)(.*)$/);
    if (asked) return span("c", esc(asked[1])) + span("r", esc(asked[2]));
    const inline = line.match(/^(\s*)([\w.]+)(\s+kept in\s+)(\S+)\s*$/);
    if (inline) return esc(inline[1]) + span("h", esc(inline[2])) +
      span("c", esc(inline[3])) + span("ok", esc(inline[4]));
    const cont = line.match(/^(\s+)([(—].*|[a-z].*)$/);
    if (cont) return esc(cont[1]) + span("c", esc(cont[2]));
    const added = line.match(/^(\s*)(wrote|added|kept|removed|stale|stub|drift|holes)(\s+)(.*)$/);
    if (added) return esc(added[1]) + span("k", esc(added[2])) + esc(added[3]) + esc(added[4]);
    if (/^\S/.test(line) && /^[a-z][\w.]*\.[A-Z]/.test(line)) return span("h", esc(line));
    return esc(line);
  }).join("\n");
}

const GO_KW = new Set(("package import func type struct interface var const return if else for range " +
  "map chan go defer switch case default nil true false error string int int64 bool byte make new panic").split(" "));

function paintGo(src) {
  let out = "", i = 0;
  while (i < src.length) {
    const ch = src[i];
    if (ch === "/" && src[i+1] === "/") { const j = src.indexOf("\n", i), k = j < 0 ? src.length : j;
      out += span("c", src.slice(i, k)); i = k; continue; }
    if (ch === '"' || ch === "` + "`" + `") { const q = ch; let j = i + 1;
      while (j < src.length && (src[j] !== q || (q === '"' && src[j-1] === "\\"))) j++;
      const text = src.slice(i, Math.min(j + 1, src.length));
      out += text.includes("tilegen:hole") ? span("h", text) : span("s", text);
      i = j + 1; continue; }
    if (/[A-Za-z_]/.test(ch)) { let j = i;
      while (j < src.length && /\w/.test(src[j])) j++;
      const word = src.slice(i, j);
      out += GO_KW.has(word) ? span("k", word) : esc(word);
      i = j; continue; }
    if (/\d/.test(ch)) { let j = i; while (j < src.length && /[\w.]/.test(src[j])) j++;
      out += span("l", src.slice(i, j)); i = j; continue; }
    out += esc(ch); i++;
  }
  return out;
}

const SQL_KW = new Set(("CREATE TABLE PRIMARY KEY NOT NULL CHECK IN SELECT FROM WHERE INSERT INTO VALUES " +
  "ON CONFLICT DO UPDATE SET ORDER BY DELETE EXISTS LIMIT AND OR AS COUNT EXCLUDED " +
  "TEXT INTEGER BIGINT UUID TIMESTAMPTZ BOOLEAN REAL BLOB BYTEA JSONB SMALLINT").split(" "));

function paintSQL(src) {
  let out = "", i = 0;
  while (i < src.length) {
    const ch = src[i];
    if (ch === "-" && src[i+1] === "-") { const j = src.indexOf("\n", i), k = j < 0 ? src.length : j;
      out += span("c", src.slice(i, k)); i = k; continue; }
    if (ch === "'") { let j = i + 1; while (j < src.length && src[j] !== "'") j++;
      out += span("s", src.slice(i, Math.min(j + 1, src.length))); i = j + 1; continue; }
    if (/[A-Za-z_]/.test(ch)) { let j = i; while (j < src.length && /\w/.test(src[j])) j++;
      const word = src.slice(i, j);
      out += SQL_KW.has(word.toUpperCase()) && word === word.toUpperCase() ? span("k", word) : esc(word);
      i = j; continue; }
    if (ch === "$" || ch === "?") { let j = i + 1; while (j < src.length && /\d/.test(src[j])) j++;
      out += span("l", src.slice(i, j)); i = j; continue; }
    out += esc(ch); i++;
  }
  return out;
}

function paintJSON(src) {
  let out = "", i = 0;
  while (i < src.length) {
    if (src[i] === '"') { let j = i + 1;
      while (j < src.length && (src[j] !== '"' || src[j-1] === "\\")) j++;
      const text = src.slice(i, Math.min(j + 1, src.length));
      i = j + 1;
      let k = i; while (k < src.length && /\s/.test(src[k])) k++;
      out += span(src[k] === ":" ? "k" : "s", text);
      continue;
    }
    out += esc(src[i]); i++;
  }
  return out;
}

const painters = {sexp:paintSexp, go:paintGo, sql:paintSQL, json:paintJSON, sh:paintAnswer};
document.querySelectorAll("pre code[data-lang]").forEach(el => {
  const paint = painters[el.dataset.lang];
  el.innerHTML = paint ? paint(el.textContent) : esc(el.textContent);
});

function tab(name) {
  for (const n of ["start", "try", "docs"]) {
    $(n).hidden = n !== name;
    $("tab-" + n).setAttribute("aria-selected", n === name);
  }
  if (name === "try") history.replaceState(null, "", location.pathname);
  else location.hash = "#" + name;
  window.scrollTo(0, 0);
}
$("tab-start").onclick = () => tab("start");
$("tab-try").onclick = () => tab("try");
$("tab-docs").onclick = () => tab("docs");
if (location.hash.startsWith("#docs")) tab("docs");
if (location.hash.startsWith("#start")) tab("start");

/* The builder. Every control maps to one thing a spec can say, so the
   preview is the answer sheet: change a box, see the line it writes. */

const plural = w => w.toLowerCase() + (/s$/.test(w.toLowerCase()) ? "es" : "s");
const checked = id => [...$(id).querySelectorAll("input:checked")].map(i => i.value);
const radio = name => (document.querySelector("input[name=" + name + "]:checked") || {}).value || "";
const ident = (s, fallback) => (s || "").trim().replace(/[^\w.\/-]/g, "") || fallback;

function buildSpec() {
  const name = ident($("f-name").value, "notes");
  const module = ident($("f-module").value, "example.com/" + name);
  const Entity = ident($("f-entity").value, "Note").replace(/[^\w]/g, "") || "Note";
  const pkg = plural(Entity);
  const idType = radio("id");
  const fields = checked("f-fields");
  let ops = checked("f-ops");
  if (!ops.length) ops = ["get"];
  const durable = $("f-durable").checked;
  const wantHTTP = $("f-http").checked, wantEvents = $("f-events").checked;
  const wantEnum = $("f-enum").checked, xproc = $("f-xproc").checked;
  const store = radio("store");

  const usesUUID = idType === "uuid.UUID" || fields.some(f => f.includes("uuid.UUID"));

  let s = "(project " + name + "\n  (module " + module + ")\n  (go 1.22)";
  s += usesUUID ? "\n  (require (uuid github.com/google/uuid v1.6.0)))\n" : ")\n";

  s += "\n(package " + pkg + "\n";
  if (wantEnum) s += "  (enum Status draft active done)\n\n";
  s += "  (entity " + Entity + "\n    (field ID " + idType + ")\n";
  for (const f of fields) s += "    (field " + f + ")\n";
  if (wantEnum) s += "    (field Status Status)\n";
  s += "    (store " + ops.join(" ");
  if (wantEnum && ops.includes("list")) s += "\n      (list-by Status)";
  if (durable) s += "\n      (durable)";
  s += "))";

  if (wantHTTP) {
    const routes = [];
    const path = "/" + pkg;
    if (ops.includes("list")) routes.push('(route GET    "' + path + '"      (list ' + Entity + "))");
    if (ops.includes("get")) routes.push('(route GET    "' + path + '/{id}" (get ' + Entity + "))");
    if (ops.includes("save")) routes.push('(route POST   "' + path + '"      (save ' + Entity + "))");
    if (ops.includes("delete")) routes.push('(route DELETE "' + path + '/{id}" (delete ' + Entity + "))");
    if (routes.length) s += "\n\n  (http\n    " + routes.join("\n    ") + ")";
  }
  if (wantEvents) {
    s += "\n\n  (events";
    if (xproc) s += "\n    (cross-process)";
    s += "\n    (event " + Entity + "Saved (field " + Entity + "ID " + idType + ")))";
  }
  s += ")\n";

  if (store) {
    const firm = radio("firm"), who = radio("who");
    const why = ($("f-why").value || "").trim().replace(/"/g, "'") || "that is what we run";
    s += "\n(policy\n  (prefer " + store;
    if (firm === "required") s += "\n    (strength required)";
    s += "\n    (source " + who + ' "' + why + '")))\n';
  }
  return s;
}

function refresh() {
  const store = radio("store");
  $("f-policy-set").hidden = !store;
  $("f-events-sub").hidden = !$("f-events").checked;
  const clash = store === "memory" && $("f-durable").checked;
  $("f-warn").hidden = !clash;
  if (clash) $("f-warn").textContent =
    "A map does not survive a restart, so tilegen will refuse this pair and " +
    "say so. Press What would it do? to see the message.";
  const spec = buildSpec();
  $("f-out").innerHTML = paintSexp(spec);
  return spec;
}

$("start").addEventListener("input", refresh);
$("start").addEventListener("change", refresh);
$("f-open").onclick = () => { $("spec").value = refresh(); tab("try"); };
refresh();

document.querySelectorAll("button.try").forEach(b => {
  b.onclick = () => {
    let el = b.previousElementSibling;
    while (el && !el.querySelector("code[data-lang=sexp]")) el = el.previousElementSibling;
    if (!el) return;
    $("spec").value = el.querySelector("code[data-lang=sexp]").textContent.trim() + "\n";
    tab("try"); $("explain").click();
  };
});

const out = $("out"), title = $("title");
const show = html => { out.innerHTML = html; };
const plain = text => { out.textContent = text; };

const post = path => fetch(path, {method:"POST", headers:{"Content-Type":"application/json"},
                                  body: JSON.stringify({spec: $("spec").value})});

$("explain").onclick = async () => {
  title.textContent = "how each store will be kept";
  plain("working…");
  try {
    const res = await post("/explain");
    const body = await res.json();
    if (!res.ok) { plain(body.error); return; }
    show(render(body));
  } catch (e) { plain(String(e)); }
};

$("download").onclick = async () => {
  title.textContent = "download";
  plain("generating…");
  try {
    const res = await post("/generate");
    if (!res.ok) { plain((await res.json()).error); return; }
    const blob = await res.blob();
    const m = (res.headers.get("Content-Disposition") || "").match(/"(.+)"/);
    const name = m ? m[1] : "project.zip";
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob); a.download = name; a.click();
    URL.revokeObjectURL(a.href);
    show("Saved " + esc(name) + ". " + (res.headers.get("X-Tilegen-Holes") || "0") +
         " methods are yours to write, listed in tilegen.tasks.json.\n\n" +
         paintGo("unzip " + name + " && cd " + name.replace(/\.zip$/, "") + "\n" +
                 "sqlc generate   // only if the project has a db/ folder\n" +
                 "go mod tidy\n" +
                 "go build ./..."));
  } catch (e) { plain(String(e)); }
};

function render(b) {
  if (!b.coverage.length) return "This spec has no stores yet, so there is nothing to decide.\n\n" +
    "Add (store get save) to an entity and try again.";
  let s = "";
  for (const c of b.coverage) {
    s += span("h", c.need) + "\n";
    if (c.requirements && c.requirements.length)
      s += "  you asked for   " + esc(c.requirements.join(", ")) + "\n";
    for (const cand of c.candidates)
      s += cand.tile === c.chosen
        ? "  kept in         " + span("k", cand.tile) + "\n"
        : "  also possible   " + esc(cand.tile) + "\n";
    for (const bad of (c.illegal || []))
      s += "  ruled out       " + esc(bad.tile) + "  " + span("c", "— " + bad.reason) + "\n";
    if (c.why) s += "  " + span("c", c.why) + "\n";
    s += "\n";
  }
  return s;
}
</script>
</html>`
