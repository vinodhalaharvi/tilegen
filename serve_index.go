package main

import (
	"net/http"
	"strings"
)

// The one page the service serves: a spec on the left, what tilegen would
// do with it on the right, and a button that downloads the project. It is
// deliberately one file with no build step, no framework and no external
// request, so the service stays a single binary.

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
<title>tilegen — scaffold a Go project from a spec</title>
<style>
  :root { color-scheme: light dark; --bg:#fff; --fg:#111; --dim:#666; --line:#ddd; --accent:#2a6; --soft:#f6f6f6; }
  @media (prefers-color-scheme: dark) { :root { --bg:#151515; --fg:#eee; --dim:#999; --line:#333; --soft:#1e1e1e; } }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--fg);
         font:14px/1.5 ui-sans-serif, system-ui, -apple-system, sans-serif; }
  header { padding:14px 20px; border-bottom:1px solid var(--line); display:flex; gap:14px; align-items:baseline; flex-wrap:wrap; }
  h1 { font-size:16px; margin:0; font-weight:600; }
  header p { margin:0; color:var(--dim); }
  header a { color:var(--dim); margin-left:auto; }
  main { display:grid; grid-template-columns:1fr 1fr; gap:0; height:calc(100vh - 108px); }
  @media (max-width:800px) { main { grid-template-columns:1fr; height:auto; } }
  section { display:flex; flex-direction:column; min-width:0; }
  section + section { border-left:1px solid var(--line); }
  .bar { padding:8px 14px; border-bottom:1px solid var(--line); color:var(--dim); display:flex; gap:10px; align-items:center; }
  textarea, pre { flex:1; margin:0; padding:14px; border:0; background:transparent; color:inherit;
        font:13px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace; overflow:auto; }
  textarea { resize:none; outline:none; min-height:50vh; }
  pre { white-space:pre-wrap; min-height:50vh; }
  button { font:inherit; padding:6px 14px; border:1px solid var(--line); border-radius:6px;
           background:transparent; color:inherit; cursor:pointer; }
  button.primary { background:var(--accent); border-color:var(--accent); color:#fff; font-weight:600; }
  button:disabled { opacity:.5; cursor:default; }
  footer { padding:10px 20px; border-top:1px solid var(--line); color:var(--dim); }
  nav { display:flex; gap:4px; }
  nav button { font:inherit; padding:5px 14px; border:1px solid transparent; border-radius:6px;
               background:transparent; color:var(--dim); cursor:pointer; }
  nav button[aria-selected=true] { background:var(--soft); color:var(--fg); border-color:var(--line); font-weight:600; }
  header a.gh { margin-left:auto; }
  article { max-width:52rem; margin:0 auto; padding:28px 22px 80px; }
  article h2 { font-size:20px; margin:2.2em 0 .5em; padding-top:.5em; border-top:1px solid var(--line); }
  article h2:first-of-type { border:0; margin-top:.4em; }
  article h3 { font-size:15px; margin:1.8em 0 .4em; }
  article pre { background:var(--soft); padding:12px 14px; border-radius:8px; overflow:auto;
                font:13px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
  article code { background:var(--soft); padding:1px 5px; border-radius:4px;
                 font:12.5px ui-monospace, SFMono-Regular, Menlo, monospace; }
  article pre code { background:none; padding:0; font-size:13px; }
  article table { border-collapse:collapse; width:100%; margin:1em 0; }
  article th, article td { text-align:left; padding:7px 10px; border-bottom:1px solid var(--line); vertical-align:top; }
  article th { color:var(--dim); font-weight:600; }
  .toc { background:var(--soft); padding:14px 18px; border-radius:8px; columns:2; }
  @media (max-width:600px) { .toc { columns:1; } }
  .toc a { display:block; color:inherit; text-decoration:none; padding:2px 0; }
  .toc a:hover { color:var(--accent); }
  .note { border-left:3px solid var(--accent); padding:2px 0 2px 14px; color:var(--dim); }
  button.try { font:inherit; padding:3px 10px; border:1px solid var(--line); border-radius:6px;
               background:transparent; color:var(--dim); cursor:pointer; font-size:12px; }
  code { color:var(--accent); }
  .err { color:#c33; }
</style>

<header>
  <h1>tilegen</h1>
  <nav>
    <button id="tab-try" aria-selected="true">Try it</button>
    <button id="tab-docs" aria-selected="false">Docs</button>
  </nav>
  <a class="gh" href="https://github.com/vinodhalaharvi/tilegen">github</a>
</header>

<main id="try">
  <section>
    <div class="bar">
      <strong>spec</strong>
      <button id="explain">what would it do?</button>
      <button id="download" class="primary">download project</button>
    </div>
    <textarea id="spec" spellcheck="false">` + exampleSpec + `</textarea>
  </section>
  <section>
    <div class="bar"><strong id="title">choices</strong></div>
    <pre id="out">Press “what would it do?” to see which implementation it picks for each store,
or “download project” to get the whole thing as a zip.

New here? Open the Docs tab.</pre>
  </section>
</main>

<div id="docs" hidden><article>` + docsHTML + `</article></div>

<footer>tilegen __VERSION__ · executes nothing: no git, no sqlc, no build · <code>tilegen serve</code></footer>

<script>
const $ = id => document.getElementById(id);
const out = $("out"), title = $("title");

function tab(name) {
  const isTry = name === "try";
  $("try").hidden = !isTry;
  $("docs").hidden = isTry;
  $("tab-try").setAttribute("aria-selected", isTry);
  $("tab-docs").setAttribute("aria-selected", !isTry);
  if (!isTry) location.hash = "#docs"; else history.replaceState(null, "", location.pathname);
  window.scrollTo(0, 0);
}
$("tab-try").onclick = () => tab("try");
$("tab-docs").onclick = () => tab("docs");
if (location.hash.startsWith("#docs")) tab("docs");

// "try this" in the docs loads that example into the editor and runs it
document.querySelectorAll("button.try").forEach(b => {
  b.onclick = () => {
    let pre = b.closest("p").previousElementSibling;
    while (pre && pre.tagName !== "PRE") pre = pre.previousElementSibling;
    if (!pre) return;
    $("spec").value = pre.innerText.trim() + "\n";
    tab("try");
    $("explain").click();
  };
});

function show(text, isErr) {
  out.textContent = text;
  out.className = isErr ? "err" : "";
}

async function post(path) {
  const res = await fetch(path, {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({spec: $("spec").value}),
  });
  return res;
}

$("explain").onclick = async () => {
  title.textContent = "choices";
  show("thinking…");
  try {
    const res = await post("/explain");
    const body = await res.json();
    if (!res.ok) { show(body.error || res.statusText, true); return; }
    show(render(body));
  } catch (e) { show(String(e), true); }
};

$("download").onclick = async () => {
  title.textContent = "download";
  show("generating…");
  try {
    const res = await post("/generate");
    if (!res.ok) { const b = await res.json(); show(b.error || res.statusText, true); return; }
    const blob = await res.blob();
    const name = (res.headers.get("Content-Disposition") || "").match(/"(.+)"/);
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name ? name[1] : "project.zip";
    a.click();
    URL.revokeObjectURL(a.href);
    show("downloaded " + a.download + "\n\n" + (res.headers.get("X-Tilegen-Holes") || "0") +
         " method(s) are left for you to write; they are listed in tilegen.tasks.json.\n\n" +
         "unzip " + a.download + " && cd " + a.download.replace(/\.zip$/, "") + "\n" +
         "sqlc generate     # only if the project has a db/ folder\n" +
         "go mod tidy\n" +
         "go build ./...");
  } catch (e) { show(String(e), true); }
};

// What tilegen decided, as a person reads it.
function render(b) {
  let s = "";
  for (const c of b.coverage) {
    s += c.need + "\n";
    if (c.requirements && c.requirements.length) {
      s += "  you asked for:  " + c.requirements.join(", ") + "\n";
    }
    for (const cand of c.candidates) {
      s += (cand.tile === c.chosen ? "  chosen:         " : "  also possible:  ") + cand.tile + "\n";
    }
    for (const bad of (c.illegal || [])) {
      s += "  ruled out:      " + bad.tile + " — " + bad.reason + "\n";
    }
    if (c.why) s += "  note:           " + c.why + "\n";
    s += "\n";
  }
  return s || "This spec has no stores, so there is nothing to choose.\n\nAdd (store get save) to an entity and try again.";
}
</script>
</html>`
