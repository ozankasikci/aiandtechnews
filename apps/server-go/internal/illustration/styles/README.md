# House illustration styles

The featured-image pipeline paints every generated image in one of these
styles. The analyzer picks one per article, using each style's `summary`.

Each style is a folder here:

```
styles/<name>/style.json   name, version, summary, prompt, anchors
styles/<name>/*.jpg        2-3 anchor reference images
```

- `name` must match the folder.
- `prompt` is added to the image prompt as `Style: <prompt>`.
- `summary` tells the analyzer which stories the style suits.
- `anchors` are attached to Codex generations as style references ("match
  the technique, brushwork, palette and finish; don't copy the subjects").
  They are not sent to Gemini.

Everything is embedded into the binary with `go:embed`, so a change ships with
the next build.

## Changing a style

1. Edit `prompt`, `summary` or the anchors.
2. Increase `version`. Logs and dry-run reports show `name@version`, so you
   can tell which images came from which version.
3. Run `go test -p 2 ./internal/illustration/...`. It checks every style
   loads and has 2-3 decodable anchors.

## Adding a style

1. Create `styles/<name>/` with a `style.json` like the existing ones.
2. Add 2-3 anchors. Pick generations you like that have no text, logos, flags
   or faces. Downscale them to about 1024px JPEG to keep the repo small:
   `sips -s format jpeg -s formatOptions 82 -Z 1024 in.png --out anchor-1.jpg`.
3. The analyzer offers every style in the folder, so no code change is needed.
   Try it with `go run ./cmd/imagegen-try` before deploying.

## Current styles

| Style | Anchors from |
|---|---|
| `gouache` | prototype runs `imagegen-test/three/{1568,17,3523}/3-gouache.png` |
| `anime` | prototype runs `imagegen-test/three/{1568,17,3523}/2-anime-painted.png` |
