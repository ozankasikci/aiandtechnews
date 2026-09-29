// Command autopick-try prints what the automatic editor would queue and
// reject for the pending candidates in a database. It opens the database
// read-only and never changes it.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/autopick"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
)

func main() {
	dbPath := flag.String("db", "", "SQLite database (opened read-only)")
	model := flag.String("model", autopick.DefaultModel, "Codex model")
	effort := flag.String("effort", autopick.DefaultEffort, "reasoning effort")
	flag.Parse()
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "-db is required")
		os.Exit(2)
	}
	db, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	runner := &illustration.CodexRunner{Bin: os.Getenv("CODEX_BIN"), NodeDir: os.Getenv("CODEX_NODE_DIR"), Timeout: autopick.DefaultTimeout}
	picker := &autopick.Picker{Store: autopick.NewSQLiteStore(db), Editor: autopick.NewCodexEditor(runner, *model, *effort), Now: time.Now}
	started := time.Now()
	run, rejected, err := picker.Plan(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan:", err)
		os.Exit(1)
	}
	fmt.Printf("%d candidates, %d to queue, %d to reject, %d undecided (%s)\n\nQUEUE\n", run.Considered, len(run.Picked), len(rejected), run.Undecided, time.Since(started).Round(time.Second))
	for i, pick := range run.Picked {
		fmt.Printf("%2d. #%d %s\n    %s\n", i+1, pick.ID, pick.Title, pick.Reason)
	}
	fmt.Println("\nREJECT")
	titles, err := titlesByID(db)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, decision := range run.Decisions {
		if !decision.Publish {
			fmt.Printf("  #%d %s\n    %s\n", decision.ID, titles[decision.ID], decision.Reason)
		}
	}
}

func titlesByID(db *sql.DB) (map[int64]string, error) {
	rows, err := db.Query(`SELECT id, title FROM candidates WHERE status = 'pending'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := map[int64]string{}
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		titles[id] = title
	}
	return titles, rows.Err()
}
