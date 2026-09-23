// Package adopt verifies that an existing SQLite database created by another
// program (the Node server) has the schema the Go migrations would create,
// and brings a compatible one under the migration ledger with
// migrate.Adopt. It never drops, rewrites, or deletes data: the only writes
// are a VACUUM INTO backup, the ledger, and the SQL of migrations whose
// schema is not present yet.
package adopt

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// Column is one column as PRAGMA table_xinfo reports it. Type and Default
// are normalized (see normalizeType and normalizeExpression); Default is ""
// when the column has no default.
type Column struct {
	Name    string
	Type    string
	NotNull bool
	Default string
	PK      int
	Hidden  int
}

func (c Column) describe() string {
	var parts []string
	if c.Type != "" {
		parts = append(parts, c.Type)
	}
	if c.NotNull {
		parts = append(parts, "NOT NULL")
	}
	if c.Default != "" {
		parts = append(parts, "DEFAULT "+c.Default)
	}
	return strings.Join(parts, " ")
}

// Index is one index of a table. Origin is "c" for CREATE INDEX, "u" for a
// UNIQUE constraint, and "pk" for a non-rowid PRIMARY KEY.
type Index struct {
	Name    string
	Table   string
	Unique  bool
	Origin  string
	Partial bool
	// Columns lists the key columns as "name [DESC] COLLATE coll"; an
	// expression column is "<expr>" and the SQL below decides.
	Columns []string
	Where   string
	// Expression is the normalized CREATE INDEX text when the index has an
	// expression column; empty otherwise.
	Expression string
}

func (i Index) signature() string {
	return fmt.Sprintf("unique=%t partial=%t columns=(%s) where=%q expr=%q", i.Unique, i.Partial, strings.Join(i.Columns, ", "), i.Where, i.Expression)
}

func (i Index) columnNames() string {
	names := make([]string, len(i.Columns))
	for n, column := range i.Columns {
		names[n] = strings.Fields(column)[0]
	}
	return strings.Join(names, ", ")
}

// Table is one table's comparable shape.
type Table struct {
	Name        string
	SQL         string
	Columns     map[string]Column
	ColumnOrder []string
	// ColumnDefs holds each column's full definition from the CREATE TABLE
	// text, token-normalized (keyword case, whitespace, identifier quoting,
	// comments), keyed by lowercase column name. It carries everything
	// PRAGMA table_xinfo does not: COLLATE, ON CONFLICT, GENERATED ALWAYS
	// AS, column CHECK/REFERENCES/UNIQUE clauses, DEFERRABLE.
	ColumnDefs map[string]string
	// Constraints are the table constraints (PRIMARY KEY, UNIQUE, CHECK,
	// FOREIGN KEY, with any ON CONFLICT or DEFERRABLE clause), normalized
	// and sorted.
	Constraints   []string
	Checks        []string // normalized CHECK expressions, sorted
	ForeignKeys   []string // normalized foreign key signatures, sorted
	Indexes       []Index  // every index, named and automatic
	Autoincrement bool
	WithoutRowID  bool
	Strict        bool
}

// Object is a schema object from sqlite_schema.
type Object struct {
	Type  string
	Name  string
	Table string
	SQL   string
}

// Schema is a database's inspected schema. Map keys are lowercase names
// (SQLite names are case-insensitive).
type Schema struct {
	Objects map[string]Object
	Tables  map[string]*Table
}

func objectKey(kind, name string) string { return kind + " " + strings.ToLower(name) }

// Inspect reads the schema through q only. sqlite_* internal objects and
// the migration ledger are skipped.
func Inspect(ctx context.Context, q migrate.Queryer) (*Schema, error) {
	schema := &Schema{Objects: map[string]Object{}, Tables: map[string]*Table{}}
	rows, err := q.QueryContext(ctx, `SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name <> 'schema_migrations' ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite_schema: %w", err)
	}
	var objects []Object
	for rows.Next() {
		var object Object
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan sqlite_schema: %w", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("read sqlite_schema: %w", err)
	}
	for _, object := range objects {
		schema.Objects[objectKey(object.Type, object.Name)] = object
	}
	for _, object := range objects {
		if object.Type != "table" {
			continue
		}
		table, err := inspectTable(ctx, q, object, schema.Objects)
		if err != nil {
			return nil, err
		}
		schema.Tables[strings.ToLower(object.Name)] = table
	}
	return schema, nil
}

func quoteLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func inspectTable(ctx context.Context, q migrate.Queryer, object Object, objects map[string]Object) (*Table, error) {
	table := &Table{Name: object.Name, SQL: object.SQL, Columns: map[string]Column{}}
	name := quoteLiteral(object.Name)

	rows, err := q.QueryContext(ctx, `SELECT name, type, "notnull", dflt_value, pk, hidden FROM pragma_table_xinfo(`+name+`)`)
	if err != nil {
		return nil, fmt.Errorf("inspect columns of %s: %w", object.Name, err)
	}
	for rows.Next() {
		var column Column
		var notNull int
		var defaultValue *string
		if err := rows.Scan(&column.Name, &column.Type, &notNull, &defaultValue, &column.PK, &column.Hidden); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan columns of %s: %w", object.Name, err)
		}
		column.Type = normalizeType(column.Type)
		column.NotNull = notNull != 0
		if defaultValue != nil {
			column.Default = normalizeExpression(*defaultValue)
		}
		table.Columns[strings.ToLower(column.Name)] = column
		table.ColumnOrder = append(table.ColumnOrder, column.Name)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("inspect columns of %s: %w", object.Name, err)
	}

	var withoutRowID, strict int
	if err := q.QueryRowContext(ctx, `SELECT wr, strict FROM pragma_table_list(`+name+`) WHERE schema = 'main'`).Scan(&withoutRowID, &strict); err != nil {
		return nil, fmt.Errorf("inspect options of %s: %w", object.Name, err)
	}
	table.WithoutRowID, table.Strict = withoutRowID != 0, strict != 0

	tokens := tokenize(object.SQL)
	table.ColumnDefs, table.Constraints = tableDefinitions(tokens)
	table.Checks = checkConstraints(tokens)
	table.Autoincrement = hasBareWord(tokens, "autoincrement")

	fks, err := foreignKeys(ctx, q, object.Name)
	if err != nil {
		return nil, err
	}
	table.ForeignKeys = fks

	indexes, err := tableIndexes(ctx, q, object.Name, objects)
	if err != nil {
		return nil, err
	}
	table.Indexes = indexes
	return table, nil
}

func foreignKeys(ctx context.Context, q migrate.Queryer, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, seq, "table", "from", coalesce("to", ''), on_update, on_delete, "match" FROM pragma_foreign_key_list(`+quoteLiteral(table)+`) ORDER BY id, seq`)
	if err != nil {
		return nil, fmt.Errorf("inspect foreign keys of %s: %w", table, err)
	}
	defer rows.Close()
	type fk struct {
		parent, onUpdate, onDelete, match string
		from, to                          []string
	}
	byID := map[int]*fk{}
	var ids []int
	for rows.Next() {
		var id, seq int
		var parent, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &parent, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, fmt.Errorf("scan foreign keys of %s: %w", table, err)
		}
		entry, ok := byID[id]
		if !ok {
			entry = &fk{parent: strings.ToLower(parent), onUpdate: onUpdate, onDelete: onDelete, match: match}
			byID[id] = entry
			ids = append(ids, id)
		}
		entry.from = append(entry.from, strings.ToLower(from))
		entry.to = append(entry.to, strings.ToLower(to))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect foreign keys of %s: %w", table, err)
	}
	signatures := make([]string, 0, len(ids))
	for _, id := range ids {
		entry := byID[id]
		signatures = append(signatures, fmt.Sprintf("(%s) REFERENCES %s(%s) ON UPDATE %s ON DELETE %s MATCH %s",
			strings.Join(entry.from, ", "), entry.parent, strings.Join(entry.to, ", "), entry.onUpdate, entry.onDelete, entry.match))
	}
	sort.Strings(signatures)
	return signatures, nil
}

func tableIndexes(ctx context.Context, q migrate.Queryer, table string, objects map[string]Object) ([]Index, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, "unique", origin, partial FROM pragma_index_list(`+quoteLiteral(table)+`) ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("inspect indexes of %s: %w", table, err)
	}
	var indexes []Index
	for rows.Next() {
		index := Index{Table: table}
		var unique, partial int
		if err := rows.Scan(&index.Name, &unique, &index.Origin, &partial); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan indexes of %s: %w", table, err)
		}
		index.Unique, index.Partial = unique != 0, partial != 0
		indexes = append(indexes, index)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("inspect indexes of %s: %w", table, err)
	}
	for i := range indexes {
		index := &indexes[i]
		columns, err := q.QueryContext(ctx, `SELECT cid, coalesce(name, ''), "desc", coalesce(coll, '') FROM pragma_index_xinfo(`+quoteLiteral(index.Name)+`) WHERE "key" = 1 ORDER BY seqno`)
		if err != nil {
			return nil, fmt.Errorf("inspect index %s: %w", index.Name, err)
		}
		expression := false
		for columns.Next() {
			var cid, desc int
			var name, collation string
			if err := columns.Scan(&cid, &name, &desc, &collation); err != nil {
				columns.Close()
				return nil, fmt.Errorf("scan index %s: %w", index.Name, err)
			}
			switch cid {
			case -2:
				name, expression = "<expr>", true
			case -1:
				name = "rowid"
			}
			column := strings.ToLower(name)
			if desc != 0 {
				column += " DESC"
			}
			column += " COLLATE " + strings.ToUpper(collation)
			index.Columns = append(index.Columns, column)
		}
		if err := columns.Close(); err != nil {
			return nil, fmt.Errorf("inspect index %s: %w", index.Name, err)
		}
		if object, ok := objects[objectKey("index", index.Name)]; ok && object.SQL != "" {
			tokens := tokenize(object.SQL)
			if index.Partial {
				index.Where = whereClause(tokens)
			}
			if expression {
				index.Expression = joinTokens(tokens)
			}
		}
	}
	return indexes, nil
}

// normalizeType uppercases a declared type and collapses whitespace.
func normalizeType(declared string) string {
	return strings.Join(strings.Fields(strings.ToUpper(declared)), " ")
}

// normalizeExpression token-normalizes an SQL expression and removes
// redundant outer parentheses, so "(datetime('now'))" and "datetime( 'now' )"
// compare equal while string literals stay exact.
func normalizeExpression(expression string) string {
	return normalizeTokens(tokenize(expression))
}

func normalizeTokens(tokens []token) string {
	for len(tokens) >= 2 && tokens[0].text == "(" && closingParen(tokens, 0) == len(tokens)-1 {
		tokens = tokens[1 : len(tokens)-1]
	}
	return joinTokens(tokens)
}

type tokenKind int

const (
	tokenWord   tokenKind = iota // bare keyword or identifier, lowercased
	tokenQuoted                  // quoted identifier, unquoted and lowercased
	tokenString                  // string or blob literal, verbatim
	tokenPunct                   // single punctuation character
)

type token struct {
	kind tokenKind
	text string
}

// tokenize splits SQL into tokens, dropping whitespace and comments. It is
// only used to compare schema text, never to execute it.
func tokenize(sqlText string) []token {
	var tokens []token
	for i := 0; i < len(sqlText); {
		c := sqlText[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < len(sqlText) && sqlText[i+1] == '-':
			for i < len(sqlText) && sqlText[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(sqlText) && sqlText[i+1] == '*':
			end := strings.Index(sqlText[i+2:], "*/")
			if end < 0 {
				i = len(sqlText)
			} else {
				i += end + 4
			}
		case c == '\'':
			end := scanQuoted(sqlText, i+1, '\'')
			tokens = append(tokens, token{tokenString, sqlText[i:end]})
			i = end
		case (c == 'x' || c == 'X') && i+1 < len(sqlText) && sqlText[i+1] == '\'':
			end := scanQuoted(sqlText, i+2, '\'')
			tokens = append(tokens, token{tokenString, strings.ToLower(sqlText[i:end])})
			i = end
		case c == '"' || c == '`' || c == '[':
			closing := c
			if c == '[' {
				closing = ']'
			}
			end := scanQuoted(sqlText, i+1, closing)
			inner := sqlText[i+1 : max(i+1, end-1)]
			if closing != ']' {
				inner = strings.ReplaceAll(inner, string([]byte{closing, closing}), string(closing))
			}
			tokens = append(tokens, token{tokenQuoted, strings.ToLower(inner)})
			i = end
		case isWordByte(c):
			start := i
			for i < len(sqlText) && isWordByte(sqlText[i]) {
				i++
			}
			tokens = append(tokens, token{tokenWord, strings.ToLower(sqlText[start:i])})
		default:
			tokens = append(tokens, token{tokenPunct, string(c)})
			i++
		}
	}
	return tokens
}

func scanQuoted(sqlText string, start int, closing byte) int {
	for i := start; i < len(sqlText); i++ {
		if sqlText[i] != closing {
			continue
		}
		if closing != ']' && i+1 < len(sqlText) && sqlText[i+1] == closing {
			i++
			continue
		}
		return i + 1
	}
	return len(sqlText)
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c >= 0x80
}

// joinTokens renders tokens deterministically: single spaces, none inside
// parentheses or before a comma, and none between a function name and its
// "(". Quoted identifiers render bare, so `"status"` and `status` compare
// equal; string literals stay exact.
func joinTokens(tokens []token) string {
	var b strings.Builder
	for i, t := range tokens {
		if i > 0 {
			previous := tokens[i-1]
			tight := previous.kind == tokenPunct && previous.text == "(" ||
				t.kind == tokenPunct && (t.text == ")" || t.text == ",") ||
				t.kind == tokenPunct && t.text == "(" && previous.kind == tokenWord && !isKeyword(previous.text)
			if !tight {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t.text)
	}
	return b.String()
}

// isKeyword lists keywords that may precede "(" in schema SQL, so "IN (" keeps its space.
func isKeyword(word string) bool {
	switch word {
	case "in", "and", "or", "not", "check", "exists", "is", "between", "like", "glob", "when", "then", "else", "case", "on", "as":
		return true
	}
	return false
}

func closingParen(tokens []token, open int) int {
	depth := 0
	for i := open; i < len(tokens); i++ {
		if tokens[i].kind != tokenPunct {
			continue
		}
		switch tokens[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// checkConstraints returns every CHECK(...) expression of a CREATE TABLE
// statement, normalized and sorted. Column and table constraints are treated
// alike: CHECK(role IN (...)) on a column is equivalent to the same CHECK as
// a table constraint.
func checkConstraints(tokens []token) []string {
	var checks []string
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].kind != tokenWord || tokens[i].text != "check" || tokens[i+1].text != "(" || tokens[i+1].kind != tokenPunct {
			continue
		}
		end := closingParen(tokens, i+1)
		if end < 0 {
			break
		}
		checks = append(checks, normalizeTokens(tokens[i+2:end]))
		i = end
	}
	sort.Strings(checks)
	return checks
}

func hasBareWord(tokens []token, word string) bool {
	for _, t := range tokens {
		if t.kind == tokenWord && t.text == word {
			return true
		}
	}
	return false
}

// whereClause returns the normalized text after the top-level WHERE of a
// CREATE INDEX statement.
func whereClause(tokens []token) string {
	depth := 0
	for i, t := range tokens {
		if t.kind == tokenPunct && t.text == "(" {
			depth++
		}
		if t.kind == tokenPunct && t.text == ")" {
			depth--
		}
		if depth == 0 && t.kind == tokenWord && t.text == "where" {
			return joinTokens(tokens[i+1:])
		}
	}
	return ""
}

var tableConstraintStarts = map[string]bool{"constraint": true, "primary": true, "unique": true, "check": true, "foreign": true}

// tableDefinitions splits the body of a CREATE TABLE statement at top-level
// commas into column definitions (keyed by lowercase column name) and table
// constraints, each token-normalized.
func tableDefinitions(tokens []token) (map[string]string, []string) {
	columns := map[string]string{}
	var constraints []string
	open := -1
	for i, t := range tokens {
		if t.kind == tokenPunct && t.text == "(" {
			open = i
			break
		}
	}
	if open < 0 {
		return columns, nil
	}
	end := closingParen(tokens, open)
	if end < 0 {
		end = len(tokens)
	}
	var parts [][]token
	start, depth := open+1, 0
	for i := open + 1; i < end; i++ {
		t := tokens[i]
		if t.kind != tokenPunct {
			continue
		}
		switch t.text {
		case "(":
			depth++
		case ")":
			depth--
		case ",":
			if depth == 0 {
				parts = append(parts, tokens[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, tokens[start:end])
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if part[0].kind == tokenWord && tableConstraintStarts[part[0].text] {
			constraints = append(constraints, joinTokens(part))
			continue
		}
		columns[part[0].text] = joinTokens(part)
	}
	sort.Strings(constraints)
	return columns, constraints
}
