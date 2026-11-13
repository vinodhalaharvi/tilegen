package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// The studio is the page `tilegen serve` opens on: tick what the system
// must do, watch each need go to a tile, read why, and take the project.
// It is a client of this server's own endpoints, so what it shows is what
// the command line writes.
//
// Everything it needs is in the binary: its source, React and the other
// libraries, and its typefaces. A machine with no network serves the
// whole page.

//go:embed web/src web/vendor
var studioFiles embed.FS

// studioLibs are the scripts the page loads before its own, in order.
var studioLibs = []string{
	"react.production.min.js", "react-dom.production.min.js", "htm.js",
	"highlight.min.js", "hljs-protobuf.min.js", "hljs-lisp.min.js", "jszip.min.js",
}

// studioDiagrams draws the Architecture view.
const studioDiagrams = "mermaid.min.js"

// studioFonts are the faces the stylesheet names, with the weights each
// file carries.
var studioFonts = []struct{ family, file, weight string }{
	{"Bricolage Grotesque", "bricolage-grotesque-latin-opsz-normal.woff2", "200 800"},
	{"IBM Plex Sans", "ibm-plex-sans-latin-400-normal.woff2", "400"},
	{"IBM Plex Sans", "ibm-plex-sans-latin-500-normal.woff2", "500"},
	{"IBM Plex Sans", "ibm-plex-sans-latin-600-normal.woff2", "600"},
	{"IBM Plex Mono", "ibm-plex-mono-latin-400-normal.woff2", "400"},
	{"IBM Plex Mono", "ibm-plex-mono-latin-500-normal.woff2", "500"},
	{"IBM Plex Mono", "ibm-plex-mono-latin-600-normal.woff2", "600"},
}

var studio = sync.OnceValue(func() []byte {
	src := func(name string) string {
		b, err := studioFiles.ReadFile("web/src/" + name)
		if err != nil {
			panic(err) // the file is embedded: its absence is a build mistake
		}
		return string(b)
	}
	var fonts, libs strings.Builder
	for _, f := range studioFonts {
		fonts.WriteString(`@font-face{font-family:"` + f.family + `";src:url("assets/fonts/` + f.file + `") format("woff2");font-weight:` + f.weight + ";font-display:swap}\n")
	}
	for _, l := range studioLibs {
		libs.WriteString(`<script src="assets/` + l + `"></script>` + "\n")
	}
	// The diagram library is large, so the page fetches it only when the
	// Architecture view is opened; this says where it is.
	libs.WriteString(`<script>window.TILEGEN_MERMAID = "assets/` + studioDiagrams + `";</script>` + "\n")
	// The served page has nothing recorded to open on: the server answers
	// before a reader could notice.
	page := src("page.html")
	for _, r := range [][2]string{
		{"/*FONTS*/", fonts.String()}, {"/*STYLE*/", src("style.css")}, {"<!--LIBS-->", libs.String()},
		{"/*CORE*/", src("core.js")}, {"/*RECORDED*/", "null"}, {"/*APP*/", src("app.js")},
	} {
		page = strings.Replace(page, r[0], r[1], 1)
	}
	return []byte(page)
})

func handleStudio(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(studio())
}

// studioAssets serves the libraries and typefaces under /assets/.
func studioAssets() http.Handler {
	vendor, err := fs.Sub(studioFiles, "web/vendor")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/assets/", http.FileServerFS(vendor))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || strings.HasSuffix(r.URL.Path, ".md") {
			http.NotFound(w, r) // files, not listings
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}
