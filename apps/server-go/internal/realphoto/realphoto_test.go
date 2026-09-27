package realphoto_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

// sites routes every request by host to a handler, so the tests can use real
// looking domains without any network.
type sites map[string]http.HandlerFunc

func (s sites) RoundTrip(req *http.Request) (*http.Response, error) {
	handler, ok := s[req.URL.Host]
	if !ok {
		handler = func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }
	}
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	response := recorder.Result()
	response.Request = req
	return response, nil
}

func solidPNG(t *testing.T, width, height int, fill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

var (
	red   = color.RGBA{R: 220, A: 255}
	green = color.RGBA{G: 220, A: 255}
	blue  = color.RGBA{B: 220, A: 255}
	gray  = color.RGBA{R: 128, G: 128, B: 128, A: 255}
)

func serveBytes(data []byte, mediaType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", mediaType)
		_, _ = w.Write(data)
	}
}

type fakeText struct {
	plan, official string
	prompts        []string
}

func (f *fakeText) GenerateJSON(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if strings.Contains(prompt, "OFFICIAL website") {
		return f.official, nil
	}
	return f.plan, nil
}

// fakeVision answers by the image's colour.
type fakeVision struct {
	mu       sync.Mutex
	verdicts map[string]string
	calls    int
}

func colourName(c color.Color) string {
	r, g, b, _ := c.RGBA()
	switch {
	case r > 40000 && g < 20000 && b < 20000:
		return "red"
	case g > 40000 && r < 20000 && b < 20000:
		return "green"
	case b > 40000 && r < 20000 && g < 20000:
		return "blue"
	}
	return "gray"
}

func (f *fakeVision) ReviewImage(_ context.Context, _ string, data []byte, _ map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	bounds := img.Bounds()
	name := colourName(img.At(bounds.Dx()/2, bounds.Dy()/2))
	if verdict, ok := f.verdicts[name]; ok {
		return verdict, nil
	}
	return `{"match":"different","photographic":true,"watermark":false,"text_heavy":false,"marketing_banner":false,"misleading":false,"quality":5,"alt":"x","notes":"x"}`, nil
}

func verdict(quality int, alt string) string {
	return fmt.Sprintf(`{"match":"exact","photographic":true,"watermark":false,"text_heavy":false,"marketing_banner":false,"misleading":false,"quality":%d,"alt":%q,"notes":"ok"}`, quality, alt)
}

type commonsFile struct {
	title, license, licenseURL, artist string
	width, height                      int
	thumb                              string
}

func commonsAPI(t *testing.T, files []commonsFile, queries *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "aiandtech.news") {
			t.Errorf("Commons User-Agent = %q", ua)
		}
		*queries = append(*queries, r.URL.Query().Get("gsrsearch"))
		var pages []map[string]any
		for i, file := range files {
			pages = append(pages, map[string]any{
				"title": file.title, "index": i + 1,
				"imageinfo": []map[string]any{{
					"url": file.thumb, "thumburl": file.thumb, "descriptionurl": "https://commons.wikimedia.org/wiki/" + file.title,
					"width": file.width, "height": file.height, "mime": "image/png",
					"extmetadata": map[string]any{
						"LicenseShortName": map[string]any{"value": file.license},
						"LicenseUrl":       map[string]any{"value": file.licenseURL},
						"Artist":           map[string]any{"value": file.artist},
					},
				}},
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": pages}})
	}
}

const robotPlan = `{"photographable":true,"subject":"WiCi One desk robot","kind":"robot","queries":["WiCi One robot","WiCi robot"],"maker":"WiCi","reason":"A new robot."}`

func TestFindPicksTheBestScoringUsableCommonsPhoto(t *testing.T) {
	var queries []string
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, []commonsFile{
			{title: "File:NC.jpg", license: "CC BY-NC 2.0", artist: "A", width: 2000, height: 1125, thumb: "https://upload.wikimedia.org/nc.png"},
			{title: "File:Good.jpg", license: "CC BY-SA 4.0", licenseURL: "https://creativecommons.org/licenses/by-sa/4.0", artist: `<a href="//commons.wikimedia.org/wiki/User:Jane">Jane Doe</a>`, width: 3200, height: 1800, thumb: "https://upload.wikimedia.org/good.png"},
			{title: "File:Better.jpg", license: "CC BY 2.0", licenseURL: "https://creativecommons.org/licenses/by/2.0", artist: "John Roe", width: 4000, height: 2250, thumb: "https://upload.wikimedia.org/better.png"},
			{title: "File:Portrait.jpg", license: "CC0", width: 1200, height: 1600, thumb: "https://upload.wikimedia.org/portrait.png"},
			{title: "File:Small.jpg", license: "Public domain", width: 800, height: 450, thumb: "https://upload.wikimedia.org/small.png"},
		}, &queries),
		"upload.wikimedia.org": func(w http.ResponseWriter, r *http.Request) {
			fill := map[string]color.RGBA{"/good.png": green, "/better.png": blue, "/nc.png": red}[r.URL.Path]
			serveBytes(solidPNG(t, 1600, 900, fill), "image/png")(w, r)
		},
	}
	vision := &fakeVision{verdicts: map[string]string{"green": verdict(7, "A small robot on a desk."), "blue": verdict(9, "The WiCi One robot on a shelf."), "red": verdict(10, "x")}}
	text := &fakeText{plan: robotPlan}
	finder := realphoto.NewFinder(text, vision, &http.Client{Transport: routes}, nil)

	result := finder.Find(context.Background(), realphoto.Request{Slug: "wici", Title: "WiCi One is a desk robot", Text: "..."})
	if !result.Found || result.Credit == nil {
		t.Fatalf("not found: %+v", result)
	}
	if result.Chosen.PageURL != "https://commons.wikimedia.org/wiki/File:Better.jpg" || result.Alt != "The WiCi One robot on a shelf." {
		t.Fatalf("chosen = %+v alt %q", result.Chosen, result.Alt)
	}
	want := realphoto.Credit{Kind: "commons", Text: "Photo: John Roe / CC BY 2.0, via Wikimedia Commons", URL: "https://commons.wikimedia.org/wiki/File:Better.jpg", License: "CC BY 2.0", LicenseURL: "https://creativecommons.org/licenses/by/2.0"}
	if *result.Credit != want {
		t.Fatalf("credit = %+v", *result.Credit)
	}
	if len(result.Image) == 0 {
		t.Fatal("no image bytes")
	}
	if vision.calls != 2 {
		t.Fatalf("vision calls = %d, want only the two free, big, landscape files", vision.calls)
	}
	if len(queries) != 2 || queries[0] != "filetype:bitmap WiCi One robot" {
		t.Fatalf("queries = %v", queries)
	}
	reasons := map[string]string{}
	for _, candidate := range result.Candidates {
		reasons[candidate.PageURL] = candidate.Reason
	}
	if !strings.HasPrefix(reasons["https://commons.wikimedia.org/wiki/File:NC.jpg"], "licence not allowed") ||
		!strings.HasPrefix(reasons["https://commons.wikimedia.org/wiki/File:Portrait.jpg"], "aspect") ||
		!strings.HasPrefix(reasons["https://commons.wikimedia.org/wiki/File:Small.jpg"], "too narrow") {
		t.Fatalf("reasons = %v", reasons)
	}
}

func TestFindFallsBackToTheMakersOfficialImage(t *testing.T) {
	var queries []string
	sourcePage := `<html><head><title>WiCi One review</title></head><body>
		<a href="/about">About us</a>
		<a href="https://twitter.com/wici">Twitter</a>
		<a href="https://www.amazon.co.uk/dp/123">Buy on Amazon</a>
		<a href="https://www.theverge.com/wici">The Verge</a>
		<article><p>The <a href="https://wici.ai/wici-one">WiCi One</a> is a desk robot.</p></article>
		<a href="https://nvidia.com/inception">NVIDIA Inception</a>
	</body></html>`
	officialPage := `<html><head><title>WiCi One</title>
		<meta property="og:image" content="/images/og-banner.jpg">
	</head><body>
		<img src="/images/logo.png" alt="WiCi logo">
		<img src="/images/badge-nvidia-inception.png">
		<img srcset="/images/product/hero-small.png 800w, /images/product/hero-device-ledge.png 4096w" alt="WiCi One on a ledge">
		<img src="/images/product/tall.png">
	</body></html>`
	routes := sites{
		"commons.wikimedia.org": commonsAPI(t, nil, &queries),
		"www.engadget.com":      serveBytes([]byte(sourcePage), "text/html; charset=utf-8"),
		"wici.ai": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/wici-one":
				serveBytes([]byte(officialPage), "text/html")(w, r)
			case "/images/og-banner.jpg":
				serveBytes(solidPNG(t, 1200, 630, red), "image/png")(w, r)
			case "/images/product/hero-device-ledge.png":
				serveBytes(solidPNG(t, 2400, 1350, green), "image/png")(w, r)
			case "/images/product/tall.png":
				serveBytes(solidPNG(t, 1200, 1800, blue), "image/png")(w, r)
			default:
				http.NotFound(w, r)
			}
		},
		"nvidia.com": serveBytes([]byte("<title>NVIDIA Inception</title>"), "text/html"),
	}
	vision := &fakeVision{verdicts: map[string]string{
		"red":   `{"match":"exact","photographic":true,"watermark":false,"text_heavy":true,"marketing_banner":true,"misleading":false,"quality":8,"alt":"x","notes":"banner"}`,
		"green": verdict(8, "The WiCi One robot sitting on a wooden ledge."),
	}}
	text := &fakeText{plan: robotPlan, official: `{"index":0,"maker":"WiCi","reason":"The maker's product page."}`}
	finder := realphoto.NewFinder(text, vision, &http.Client{Transport: routes}, nil)

	result := finder.Find(context.Background(), realphoto.Request{Slug: "wici", Title: "WiCi One", Text: "...", SourceURL: "https://www.engadget.com/wici-one-review"})
	if !result.Found {
		t.Fatalf("not found: %s %v %+v", result.Reason, result.Notes, result.Candidates)
	}
	want := realphoto.Credit{Kind: "official", Text: "Image: WiCi", URL: "https://wici.ai/wici-one"}
	if *result.Credit != want || result.Chosen.ImageURL != "https://wici.ai/images/product/hero-device-ledge.png" {
		t.Fatalf("credit %+v chosen %+v", *result.Credit, result.Chosen)
	}
	var choosePrompt string
	for _, prompt := range text.prompts {
		if strings.Contains(prompt, "OFFICIAL website") {
			choosePrompt = prompt
		}
	}
	for _, unwanted := range []string{"twitter.com", "amazon", "theverge", "engadget.com/about"} {
		if strings.Contains(choosePrompt, unwanted) {
			t.Errorf("link prompt offers %s:\n%s", unwanted, choosePrompt)
		}
	}
	if !strings.Contains(choosePrompt, "0. domain: wici.ai | link text: WiCi One") || !strings.Contains(choosePrompt, "page title: NVIDIA Inception") {
		t.Errorf("link prompt:\n%s", choosePrompt)
	}
	if strings.Contains(result.Credit.Text+result.Credit.URL+result.Alt, "engadget") {
		t.Fatal("the source leaked into the credit")
	}
}

func TestFindStopsWhenNothingIsPhotographable(t *testing.T) {
	text := &fakeText{plan: `{"photographable":false,"subject":"","kind":"none","queries":[],"maker":"","reason":"A policy story."}`}
	vision := &fakeVision{}
	finder := realphoto.NewFinder(text, vision, &http.Client{Transport: sites{}}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "EU passes AI rules", Text: "..."})
	if result.Found || !strings.Contains(result.Reason, "no photographable subject") || vision.calls != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestFindFallsThroughOnErrors(t *testing.T) {
	routes := sites{
		"commons.wikimedia.org": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) },
	}
	finder := realphoto.NewFinder(&fakeText{plan: robotPlan}, &fakeVision{}, &http.Client{Transport: routes}, nil)
	result := finder.Find(context.Background(), realphoto.Request{Title: "WiCi One", Text: "...", SourceURL: "file:///etc/passwd"})
	if result.Found || len(result.Notes) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Notes[1], "only absolute http(s) URLs") {
		t.Fatalf("notes = %v", result.Notes)
	}
}

func TestParsePlan(t *testing.T) {
	plan, err := realphoto.ParsePlan("```json\n" + `{"photographable":true,"subject":" Tesla  Optimus ","queries":["\"Tesla Optimus\"","Optimus robot","extra"],"maker":"Tesla"}` + "\n```")
	if err != nil || plan.Subject != "Tesla Optimus" || len(plan.Queries) != 2 || plan.Queries[0] != "Tesla Optimus" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	plan, err = realphoto.ParsePlan(`{"photographable":true,"subject":"Waymo Zeekr robotaxi","queries":[]}`)
	if err != nil || len(plan.Queries) != 1 || plan.Queries[0] != "Waymo Zeekr robotaxi" {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	if _, err := realphoto.ParsePlan(`{"photographable":true,"subject":""}`); err == nil {
		t.Fatal("accepted a photographable plan without a subject")
	}
	if _, err := realphoto.ParsePlan(`nope`); err == nil {
		t.Fatal("accepted garbage")
	}
}

func TestFreeLicense(t *testing.T) {
	for name, want := range map[string]string{
		"CC BY-SA 4.0": "cc by-sa", "CC BY 2.0": "cc by", "CC BY-SA 3.0 de": "cc by-sa", "CC0": "cc0", "CC0 1.0": "cc0",
		"Public domain": "public domain",
	} {
		if got, ok := realphoto.FreeLicense(name); !ok || got != want {
			t.Errorf("FreeLicense(%q) = %q, %t", name, got, ok)
		}
	}
	for _, name := range []string{"CC BY-NC 2.0", "CC BY-ND 4.0", "CC BY-NC-SA 3.0", "GFDL", "", "Copyrighted", "CC BY-SA"} {
		if _, ok := realphoto.FreeLicense(name); ok {
			t.Errorf("FreeLicense(%q) accepted", name)
		}
	}
}

func TestOutboundLinksAndPageImages(t *testing.T) {
	page := []byte(`<a href="https://example.com/x">own</a><a href="https://www.reuters.com/y">news</a>
		<a href="https://maker.io/p"><img alt="Maker product"></a><a href="https://maker.io/q">q</a><a href="https://maker.io/r">r</a>
		<a href="javascript:alert(1)">js</a><a href="https://bit.ly/z">short</a><a href="https://store.ebay.de/x">ebay</a>`)
	links := realphoto.OutboundLinks(page, "https://www.example.com/story")
	if len(links) != 2 || links[0].URL != "https://maker.io/p" || links[0].Text != "Maker product" || links[1].URL != "https://maker.io/q" {
		t.Fatalf("links = %+v", links)
	}
	images := realphoto.PageImages([]byte(`<meta name="twitter:image" content="https://cdn.maker.io/tw.jpg">
		<img src="/a.svg"><img src="/icons/favicon.png"><img src="/hero.jpg" width="200"><img data-src="/lazy.jpg">
		<picture><source srcset="/p-1x.webp 1x, /p-2x.webp 2x"></picture>`), "https://maker.io/p")
	want := []string{"https://cdn.maker.io/tw.jpg", "https://maker.io/lazy.jpg", "https://maker.io/p-2x.webp"}
	if strings.Join(images, " ") != strings.Join(want, " ") {
		t.Fatalf("images = %v", images)
	}
}

func TestVerifyPromptNamesTheSubject(t *testing.T) {
	prompt := realphoto.VerifyPrompt(realphoto.Plan{Subject: "WiCi One desk robot", Maker: "WiCi"}, "Headline", realphoto.KindOfficial)
	if !strings.Contains(prompt, "WiCi One desk robot") || !strings.Contains(prompt, "official website") || !strings.Contains(prompt, "photorealistic official product") {
		t.Fatal(prompt)
	}
	if _, err := realphoto.ParseVerdict(`{"match":"exact"}`); err == nil {
		t.Fatal("accepted a verdict without a quality score")
	}
}
