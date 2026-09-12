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
  :root { color-scheme: light dark; --bg:#fff; --fg:#111; --dim:#666; --line:#ddd; --accent:#2a6; }
  @media (prefers-color-scheme: dark) { :root { --bg:#151515; --fg:#eee; --dim:#999; --line:#333; } }
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
  code { color:var(--accent); }
  .err { color:#c33; }
</style>

<header>
  <h1>tilegen</h1>
  <p>A spec in, a scaffolded Go project out. Everything it cannot know is left as a typed hole.</p>
  <a href="https://github.com/vinodhalaharvi/tilegen">github</a>
</header>

<main>
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
    <pre id="out">Press “what would it do?” to see which tile covers each need, what it costs, and why the others were rejected.</pre>
  </section>
</main>

<footer>tilegen __VERSION__ · executes nothing: no git, no sqlc, no build · <code>tilegen serve</code></footer>

<script>
const $ = id => document.getElementById(id);
const out = $("out"), title = $("title");

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
         " open hole(s): see tilegen.tasks.json inside.");
  } catch (e) { show(String(e), true); }
};

// The explain payload, as a person reads it.
function render(b) {
  let s = "weights: " + Object.entries(b.policy.weights).map(([k,v]) => k+" "+v).join(", ") + "\n";
  for (const c of b.coverage) {
    s += "\n" + c.need + "   (" + c.capability + ")";
    if (c.requirements && c.requirements.length) s += "   needs: " + c.requirements.join(", ");
    s += "   chosen by: " + c.chosen_by + "\n";
    for (const cand of c.candidates) {
      const mark = cand.tile === c.chosen ? "chosen  " : "        ";
      s += "  " + mark + cand.tile.padEnd(15) + " score " + String(cand.score).padEnd(4) + " " + (cand.terms || "");
      if (cand.chain && cand.chain.length) {
        s += " = " + cand.own;
        for (const st of cand.chain) s += "\n" + " ".repeat(36) + "+ " + st.rule + " " + st.score + " (" + st.detail + ")";
      }
      s += "\n";
    }
    for (const bad of (c.illegal || [])) s += "  illegal " + bad.tile.padEnd(15) + " " + bad.reason + "\n";
    if (c.why) s += "  policy: " + c.why + "\n";
  }
  return s;
}
</script>
</html>`
