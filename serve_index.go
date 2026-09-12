package main

import (
	"net/http"
	"strings"
)

// The pages the service serves: a spec editor, and the documentation.
// One file, no build step, no external request, so the service stays a
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
      (durable)))                 ; must survive a restart

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
`

const indexHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tilegen — describe a Go service, get the project</title>
<style>
  :root {
    --ink:#172033; --paper:#fcfcfa; --rule:#d9dde5; --dim:#5b657a;
    --chosen:#2f6f8f; --mark:#a8442a; --panel:#f3f4f1; --shade:#eceef2;
    --kw:#2f6f8f; --str:#4a6b3d; --note:#8a93a3;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --ink:#e6eaf2; --paper:#101722; --rule:#2a3343; --dim:#929cb0;
      --chosen:#7fb6d4; --mark:#e08a6d; --panel:#18202c; --shade:#1c2533;
      --kw:#7fb6d4; --str:#9dc183; --note:#6f7a8c;
    }
  }
  * { box-sizing:border-box; }
  html { scroll-behavior:smooth; }
  @media (prefers-reduced-motion:reduce) { html { scroll-behavior:auto; } }
  body { margin:0; background:var(--paper); color:var(--ink);
         font:16px/1.65 ui-serif, Iowan Old Style, Palatino, Georgia, serif;
         -webkit-font-smoothing:antialiased; }
  a { color:var(--chosen); }
  :focus-visible { outline:2px solid var(--chosen); outline-offset:2px; }

  /* ---- chrome ---- */
  .top { display:flex; align-items:baseline; gap:20px; padding:14px 24px;
         border-bottom:1px solid var(--rule); flex-wrap:wrap; }
  .wordmark { font:600 17px/1 ui-monospace, SFMono-Regular, Menlo, monospace; letter-spacing:-.2px; }
  .tag { color:var(--dim); font-size:14px; }
  .top nav { display:flex; gap:2px; margin-left:auto; }
  .top nav button { font:inherit; font-size:14px; padding:5px 14px; cursor:pointer;
        border:1px solid transparent; border-radius:4px; background:none; color:var(--dim); }
  .top nav button[aria-selected=true] { color:var(--ink); border-color:var(--rule); background:var(--panel); }
  .top a.src { color:var(--dim); font-size:14px; text-decoration:none; }
  .top a.src:hover { color:var(--chosen); }

  /* ---- try it ---- */
  #try { display:grid; grid-template-columns:1fr 1fr; height:calc(100vh - 106px); }
  #try > div { display:flex; flex-direction:column; min-width:0; }
  #try > div + div { border-left:1px solid var(--rule); }
  .bar { display:flex; gap:10px; align-items:center; padding:9px 16px;
         border-bottom:1px solid var(--rule); color:var(--dim); font-size:13px; }
  .bar strong { font:600 13px/1 ui-monospace, monospace; color:var(--ink); }
  .bar .fill { margin-left:auto; }
  #spec, #out { flex:1; margin:0; padding:16px 18px; border:0; overflow:auto;
        background:none; color:var(--ink);
        font:13px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; tab-size:2; }
  #spec { resize:none; outline:none; }
  #out { white-space:pre-wrap; }
  @media (max-width:860px) {
    #try { grid-template-columns:1fr; height:auto; }
    #try > div + div { border-left:0; border-top:1px solid var(--rule); }
    #spec { min-height:46vh; } #out { min-height:30vh; }
  }
  .btn { font:inherit; font-size:13px; padding:5px 13px; border:1px solid var(--rule);
         border-radius:4px; background:none; color:var(--ink); cursor:pointer; }
  .btn:hover { border-color:var(--chosen); color:var(--chosen); }
  .btn.go { background:var(--chosen); border-color:var(--chosen); color:var(--paper); }
  .btn.go:hover { color:var(--paper); opacity:.9; }

  /* ---- docs ---- */
  #docs { padding:0 24px 96px; }
  .doc { max-width:66rem; margin:0 auto; }
  .lede { max-width:38rem; margin:42px 0 8px; }
  .lede h1 { font-size:30px; line-height:1.25; margin:0 0 12px; font-weight:600; letter-spacing:-.3px; }
  .lede p { color:var(--dim); margin:0 0 10px; }
  .doc h2 { font-size:21px; font-weight:600; margin:56px 0 6px; padding-top:14px;
            border-top:1px solid var(--rule); letter-spacing:-.2px; }
  .doc h3 { font-size:16px; font-weight:600; margin:30px 0 6px; }
  .doc p, .doc li { max-width:38rem; }
  .doc p { margin:10px 0; }
  .doc ul { max-width:38rem; padding-left:20px; }
  code { font:13px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace;
         background:var(--shade); padding:1px 5px; border-radius:3px; }
  pre { margin:0; padding:14px 16px; background:var(--panel); border-radius:6px; overflow:auto; }
  pre code { background:none; padding:0; font-size:12.5px; line-height:1.6; display:block; }

  /* the page's one structural idea: what you write, and what that gives you */
  .pair { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:14px; margin:16px 0 8px; }
  @media (max-width:860px) { .pair { grid-template-columns:1fr; } }
  .pair > figure { margin:0; min-width:0; }
  .pair figcaption, .single figcaption { font:12px/1 ui-monospace, monospace; color:var(--dim);
        margin:0 0 6px; padding-left:2px; }
  .single { margin:16px 0 8px; }
  .caption { color:var(--dim); font-size:14px; margin:6px 0 0; }

  .toc { columns:2; column-gap:32px; max-width:44rem; margin:14px 0 6px; padding:0; list-style:none; }
  @media (max-width:620px) { .toc { columns:1; } }
  .toc a { display:block; padding:3px 0; text-decoration:none; color:var(--ink); font-size:14px; }
  .toc a:hover { color:var(--chosen); }
  .toc span { color:var(--dim); }

  table { border-collapse:collapse; width:100%; max-width:44rem; margin:14px 0; font-size:14px; }
  th, td { text-align:left; padding:7px 12px 7px 0; border-bottom:1px solid var(--rule); vertical-align:top; }
  th { color:var(--dim); font-weight:600; font-size:13px; }
  td code { font-size:12.5px; }
  .aside { border-left:2px solid var(--rule); padding:2px 0 2px 16px; color:var(--dim);
           max-width:38rem; margin:16px 0; }
  .try { font:inherit; font-size:13px; padding:4px 12px; border:1px solid var(--rule);
         border-radius:4px; background:none; color:var(--dim); cursor:pointer; margin-top:10px; }
  .try:hover { border-color:var(--chosen); color:var(--chosen); }

  /* syntax */
  .c { color:var(--note); font-style:italic; }
  .s { color:var(--str); }
  .k { color:var(--kw); }
  .h { color:var(--mark); font-weight:600; }
  .foot { border-top:1px solid var(--rule); margin-top:56px; padding:14px 24px;
          color:var(--dim); font-size:13px; }
</style>

<div class="top">
  <span class="wordmark">tilegen</span>
  <span class="tag">describe a Go service, get the project</span>
  <nav>
    <button id="tab-try" aria-selected="true">Try it</button>
    <button id="tab-docs" aria-selected="false">Docs</button>
  </nav>
  <a class="src" href="https://github.com/vinodhalaharvi/tilegen">source</a>
</div>

<main id="try">
  <div>
    <div class="bar">
      <strong>spec</strong>
      <span class="fill"></span>
      <button class="btn" id="explain">What would it do?</button>
      <button class="btn go" id="download">Download project</button>
    </div>
    <textarea id="spec" spellcheck="false">` + exampleSpec + `</textarea>
  </div>
  <div>
    <div class="bar"><strong id="title">result</strong></div>
    <pre id="out">Edit the spec, then press <b>What would it do?</b> to see how each store
will be kept, or <b>Download project</b> for the whole thing as a zip.

First time here? Open <b>Docs</b>.</pre>
  </div>
</main>

<div id="docs" hidden><div class="doc">` + docsHTML + `</div></div>

<div class="foot">tilegen __VERSION__ · <code>go install github.com/vinodhalaharvi/tilegen@latest</code></div>

<script>
const $ = id => document.getElementById(id);

/* ---- syntax colouring, small on purpose ---- */
const esc = s => s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");

function sexp(src) {
  let out = "", i = 0;
  while (i < src.length) {
    const ch = src[i];
    if (ch === ";") { const j = src.indexOf("\n", i); const k = j < 0 ? src.length : j;
      out += '<span class="c">' + esc(src.slice(i, k)) + "</span>"; i = k; continue; }
    if (ch === '"') { let j = i + 1; while (j < src.length && (src[j] !== '"' || src[j-1] === "\\")) j++;
      out += '<span class="s">' + esc(src.slice(i, j + 1)) + "</span>"; i = j + 1; continue; }
    if (ch === "(") { let j = i + 1; while (j < src.length && /[\w\-\/]/.test(src[j])) j++;
      out += "(" + '<span class="k">' + esc(src.slice(i + 1, j)) + "</span>"; i = j; continue; }
    out += esc(ch); i++;
  }
  return out;
}

const goKeywords = /\b(package|import|func|type|struct|interface|var|const|return|if|else|for|range|map|chan|go|defer|switch|case|default|nil|error|panic)\b/g;
function golang(src) {
  return esc(src)
    .replace(/(\/\/[^\n]*)/g, '<span class="c">$1</span>')
    .replace(/(&#34;|")([^"\n]*)(&#34;|")/g, '<span class="s">$1$2$3</span>')
    .replace(goKeywords, '<span class="k">$&</span>')
    .replace(/panic\(<span class="s">[^<]*tilegen:hole[^<]*<\/span>\)/g, '<span class="h">$&</span>');
}
function sql(src) {
  return esc(src)
    .replace(/(--[^\n]*)/g, '<span class="c">$1</span>')
    .replace(/\b(CREATE|TABLE|PRIMARY|KEY|NOT|NULL|TEXT|INTEGER|BIGINT|UUID|TIMESTAMPTZ|CHECK|IN|SELECT|FROM|WHERE|INSERT|INTO|VALUES|ON|CONFLICT|DO|UPDATE|SET|ORDER|BY|DELETE|EXISTS)\b/g, '<span class="k">$&</span>')
    .replace(/('[^']*')/g, '<span class="s">$1</span>');
}
const painters = {sexp, go: golang, sql, json: esc, sh: esc};
document.querySelectorAll("pre code[data-lang]").forEach(el => {
  const paint = painters[el.dataset.lang] || esc;
  el.innerHTML = paint(el.textContent);
});

/* ---- tabs ---- */
function tab(name) {
  const isTry = name === "try";
  $("try").hidden = !isTry;
  $("docs").hidden = isTry;
  $("tab-try").setAttribute("aria-selected", isTry);
  $("tab-docs").setAttribute("aria-selected", !isTry);
  if (isTry) history.replaceState(null, "", location.pathname); else location.hash = "#docs";
  window.scrollTo(0, 0);
}
$("tab-try").onclick = () => tab("try");
$("tab-docs").onclick = () => tab("docs");
if (location.hash.startsWith("#docs")) tab("docs");

/* a docs example goes straight into the editor */
document.querySelectorAll("button.try").forEach(b => {
  b.onclick = () => {
    const fig = b.previousElementSibling;
    const code = fig.querySelector("code[data-lang=sexp]") || fig.querySelector("code");
    if (!code) return;
    $("spec").value = code.textContent.trim() + "\n";
    tab("try");
    $("explain").click();
  };
});

/* ---- the service ---- */
const out = $("out"), title = $("title");
function show(html) { out.innerHTML = html; }
function plain(text) { out.textContent = text; }

async function post(path) {
  return fetch(path, {method:"POST", headers:{"Content-Type":"application/json"},
                      body: JSON.stringify({spec: $("spec").value})});
}

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
    const dir = name.replace(/\.zip$/, "");
    show("Saved " + esc(name) + ". " + (res.headers.get("X-Tilegen-Holes") || "0") +
         " methods are yours to write; they are listed in tilegen.tasks.json.\n\n" +
         golang("unzip " + name + " && cd " + dir + "\n" +
                "sqlc generate   // only if the project has a db/ folder\n" +
                "go mod tidy\n" +
                "go build ./..."));
  } catch (e) { plain(String(e)); }
};

function render(b) {
  if (!b.coverage.length) return "This spec has no stores yet, so there is nothing to decide.\n\nAdd " +
    esc("(store get save)") + " to an entity and try again.";
  let s = "";
  for (const c of b.coverage) {
    s += "<b>" + esc(c.need) + "</b>\n";
    if (c.requirements && c.requirements.length)
      s += '  you asked for  <span class="h">' + esc(c.requirements.join(", ")) + "</span>\n";
    for (const cand of c.candidates)
      s += cand.tile === c.chosen
        ? '  kept in       <span class="k">' + esc(cand.tile) + "</span>\n"
        : "  also possible " + esc(cand.tile) + "\n";
    for (const bad of (c.illegal || []))
      s += '  ruled out     ' + esc(bad.tile) + ' <span class="c">— ' + esc(bad.reason) + "</span>\n";
    if (c.why) s += '  <span class="c">' + esc(c.why) + "</span>\n";
    s += "\n";
  }
  return s;
}
</script>
</html>`
