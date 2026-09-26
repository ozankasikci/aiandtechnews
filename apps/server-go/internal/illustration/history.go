package illustration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Choice is the look of one published featured image.
type Choice struct {
	Style       string `json:"style"`
	Palette     string `json:"palette"`
	Composition string `json:"composition"`
}

// History remembers the latest choices, so consecutive articles vary in
// style, palette and composition.
type History interface {
	Recent(ctx context.Context) ([]Choice, error)
	Record(ctx context.Context, choice Choice) error
}

const (
	historyKey  = "newsroom.featured_image_history"
	historySize = 10
)

// SettingsHistory keeps the history as JSON in the settings table, under a
// "newsroom." key the dashboard never shows. Newest last.
type SettingsHistory struct{ db *sql.DB }

func NewSettingsHistory(db *sql.DB) *SettingsHistory { return &SettingsHistory{db: db} }

func (h *SettingsHistory) Recent(ctx context.Context) ([]Choice, error) {
	var raw string
	err := h.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, historyKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read featured image history: %w", err)
	}
	var choices []Choice
	if err := json.Unmarshal([]byte(raw), &choices); err != nil {
		// A damaged history only costs variety; start over.
		return nil, nil
	}
	return choices, nil
}

func (h *SettingsHistory) Record(ctx context.Context, choice Choice) error {
	choices, err := h.Recent(ctx)
	if err != nil {
		return err
	}
	choices = append(choices, choice)
	if len(choices) > historySize {
		choices = choices[len(choices)-historySize:]
	}
	raw, err := json.Marshal(choices)
	if err != nil {
		return err
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, historyKey, string(raw))
	if err != nil {
		return fmt.Errorf("record featured image history: %w", err)
	}
	return nil
}

// How far back each kind of repeat is avoided.
const (
	avoidRecentStyles   = 1
	avoidRecentPalettes = 3
	balanceRun          = 2 // this many of one composition in a row nudges towards the other
)

// Variety turns recent choices into what the next image should avoid or prefer.
type Variety struct {
	AvoidStyles       []string
	AvoidPalettes     []string
	PreferComposition string
}

func VarietyFrom(recent []Choice) Variety {
	var v Variety
	last := func(n int) []Choice {
		if len(recent) < n {
			return recent
		}
		return recent[len(recent)-n:]
	}
	for _, choice := range last(avoidRecentStyles) {
		if choice.Style != "" {
			v.AvoidStyles = append(v.AvoidStyles, choice.Style)
		}
	}
	for _, choice := range last(avoidRecentPalettes) {
		if choice.Palette != "" {
			v.AvoidPalettes = append(v.AvoidPalettes, choice.Palette)
		}
	}
	if run := last(balanceRun); len(run) == balanceRun {
		same := true
		for _, choice := range run {
			same = same && choice.Composition == run[0].Composition
		}
		switch {
		case same && run[0].Composition == CompositionScene:
			v.PreferComposition = CompositionSimple
		case same && run[0].Composition == CompositionSimple:
			v.PreferComposition = CompositionScene
		}
	}
	return v
}
