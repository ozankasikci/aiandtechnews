package main

import (
	"html/template"
	"io"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

// reportRow is one article of the report.
type reportRow struct {
	Slug, Title string
	// Current is the article's published featured image.
	Current string
	Found   bool
	// Image is the file name of the chosen photo, cropped, in the report folder.
	Image      string
	Credit     string
	CreditURL  string
	StandIn    string
	Verdict    *realphoto.Verdict
	Cropped    bool
	Reason     string
	Candidates []realphoto.Candidate
}

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"found": func(rows []reportRow) int {
		count := 0
		for _, row := range rows {
			if row.Found {
				count++
			}
		}
		return count
	},
}).Parse(`<!doctype html>
<meta charset="utf-8">
<title>Featured photo dry run</title>
<style>
body{font:14px/1.45 system-ui,sans-serif;background:#1a1a1a;color:#eee;margin:24px}
h1{font-size:20px} h2{font-size:16px;margin:28px 0 8px}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:12px;max-width:1100px}
.pair img{width:100%;aspect-ratio:16/9;object-fit:cover;background:#333;display:block}
.label{font-size:11px;color:#999;text-transform:uppercase;letter-spacing:.08em;margin-bottom:4px}
.none{aspect-ratio:16/9;display:flex;align-items:center;justify-content:center;background:#262626;color:#999;padding:12px;text-align:center}
.meta{color:#bbb;margin:6px 0}
table{border-collapse:collapse;max-width:1100px;width:100%;font-size:12px;margin-top:6px}
td{border-top:1px solid #333;padding:4px 8px 4px 0;vertical-align:top;color:#bbb;word-break:break-all}
a{color:#c4a1ff}
</style>
<h1>{{found .}} of {{len .}} articles got a photo</h1>
{{range .}}
<h2>{{.Title}}</h2>
<div class="pair">
  <div><div class="label">Current</div><img src="{{.Current}}" alt=""></div>
  <div><div class="label">Real photo</div>
  {{if .Found}}<img src="{{.Image}}" alt="{{with .Verdict}}{{.Alt}}{{end}}">{{else}}<div class="none">{{.Reason}}</div>{{end}}</div>
</div>
{{if .Found}}
<p class="meta"><a href="{{.CreditURL}}">{{.Credit}}</a>{{if .StandIn}} · Stand-in: {{.StandIn}}{{end}} · Cropped: {{if .Cropped}}yes{{else}}no{{end}}</p>
{{with .Verdict}}<p class="meta">Match: {{.Match}} · Dated: {{.Dated}} · Quality: {{.Quality}} · Alt: {{.Alt}} · Notes: {{.Notes}}</p>{{end}}
{{end}}
{{if .Candidates}}<table>
{{range .Candidates}}<tr><td>{{.Source}}</td><td><a href="{{.PageURL}}">{{.PageURL}}</a></td><td>{{if eq .Source "commons"}}Capture year: {{if .CaptureYear}}{{.CaptureYear}}{{else}}unknown{{end}}{{end}}</td><td>{{if .Usable}}usable{{if .Verdict}}, quality {{.Verdict.Quality}}{{end}}{{else}}{{.Reason}}{{end}}</td></tr>
{{end}}</table>{{end}}
{{end}}
`))

// renderReport writes the dry-run report as one HTML page.
func renderReport(w io.Writer, rows []reportRow) error {
	return reportTemplate.Execute(w, rows)
}
