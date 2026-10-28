//go:build !lite
package browser

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// Topology eviction: edges are append-only (every click/navigate inserts one),
// so without pruning the table grows unbounded. We debounce pruning to at most
// once per pruneInterval and cap the number of edges returned by map queries.
const (
	edgeTTL       = 30 * 24 * time.Hour // delete edges older than this
	pruneInterval = 1 * time.Hour       // prune at most once per interval
	maxMapEdges   = 10000               // cap edges returned by GetFullMap/GetDomainMap
)

type Node struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"` // SaaS: Owner of this knowledge
	Domain         string `json:"domain"`
	URL            string `json:"url"`
	Title          string `json:"title"`
	IsDeadEnd      bool   `json:"is_dead_end"`
	ContentAnchors string `json:"content_anchors,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

type Edge struct {
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"` // Track specific task runs
	Action    string `json:"action"`
	Args      string `json:"args"`
	CreatedAt int64  `json:"created_at"`
}

type Topology struct {
	mu        *sync.Mutex
	db        *sql.DB
	userID    string // Current user context
	sessionID string // Current run context
	lastPrune time.Time

	// ROI counters (atomic, zero overhead)
	deadEndHits  atomic.Int64
	mapQueries   atomic.Int64
	transitions  atomic.Int64
	anchorsSaved atomic.Int64
	anchorsRead  atomic.Int64
}

// TopologyStats holds ROI counters for evaluating whether the topology
// feature is consuming more than it produces.
type TopologyStats struct {
	DeadEndHits  int64 `json:"dead_end_hits"`
	MapQueries   int64 `json:"map_queries"`
	Transitions  int64 `json:"transitions"`
	AnchorsSaved int64 `json:"anchors_saved"`
	AnchorsRead  int64 `json:"anchors_read"`
}

// Stats returns a snapshot of the ROI counters.
func (t *Topology) Stats() TopologyStats {
	return TopologyStats{
		DeadEndHits:  t.deadEndHits.Load(),
		MapQueries:   t.mapQueries.Load(),
		Transitions:  t.transitions.Load(),
		AnchorsSaved: t.anchorsSaved.Load(),
		AnchorsRead:  t.anchorsRead.Load(),
	}
}

func NewTopology(dbPath string, userID string) (*Topology, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	// SQLite only allows one writer — prevent "database is locked" errors.
	db.SetMaxOpenConns(1)

	// Ensure schema is up to date (Migration)
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration failed: %w", err)
	}

	return &Topology{
		mu:        &sync.Mutex{},
		db:        db,
		userID:    userID,
		sessionID: fmt.Sprintf("sess_%d", time.Now().Unix()),
	}, nil
}

func ensureSchema(db *sql.DB) error {
	// 1. Check if 'nodes' table exists
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='nodes'").Scan(&tableName)
	if err != nil {
		// Fresh install - just create tables
		return initFresh(db)
	}

	// 2. Check if user_id exists (indicates current schema)
	cols, err := getTableColumns(db, "nodes")
	if err != nil {
		return err
	}

	if !cols["user_id"] {
		// Migration required
		if err := migrateOldSchema(db, cols); err != nil {
			return err
		}
		cols, _ = getTableColumns(db, "nodes")
	}

	if !cols["content_anchors"] {
		if _, err := db.Exec("ALTER TABLE nodes ADD COLUMN content_anchors TEXT DEFAULT ''"); err != nil {
			log.Printf("topology migration: ADD COLUMN content_anchors failed: %v", err)
		}
	}

	// 3. Always ensure indexes
	return ensureIndexes(db)
}

func getTableColumns(db *sql.DB, table string) (map[string]bool, error) {
	res := make(map[string]bool)
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dtype, dfltVal string
		var notnull, pk int
		rows.Scan(&cid, &name, &dtype, &notnull, &dfltVal, &pk)
		res[name] = true
	}
	return res, nil
}

func initFresh(db *sql.DB) error {
	initSQL := `
	CREATE TABLE IF NOT EXISTS nodes (
		id TEXT,
		user_id TEXT,
		domain TEXT,
		url TEXT,
		title TEXT,
		is_dead_end INTEGER DEFAULT 0,
		created_at INTEGER,
		PRIMARY KEY(id, user_id)
	);
	CREATE TABLE IF NOT EXISTS edges (
		from_id TEXT,
		to_id TEXT,
		user_id TEXT,
		session_id TEXT,
		action TEXT,
		args TEXT,
		created_at INTEGER
	);
	`
	if _, err := db.Exec(initSQL); err != nil {
		return err
	}
	return ensureIndexes(db)
}

func migrateOldSchema(db *sql.DB, oldNodeCols map[string]bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// ── Migrate Nodes ────────────────────────────────────────────────────────
	if _, err := tx.Exec("ALTER TABLE nodes RENAME TO nodes_old"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE nodes (
			id TEXT,
			user_id TEXT,
			domain TEXT,
			url TEXT,
			title TEXT,
			is_dead_end INTEGER DEFAULT 0,
			created_at INTEGER,
			PRIMARY KEY(id, user_id)
		)`); err != nil {
		return err
	}

	nodeSelect := "id, 'local_user'"
	for _, col := range []string{"domain", "url", "title", "is_dead_end", "created_at"} {
		if oldNodeCols[col] {
			nodeSelect += ", " + col
		} else {
			if col == "is_dead_end" {
				nodeSelect += ", 0"
			} else if col == "created_at" {
				nodeSelect += ", strftime('%s','now')"
			} else {
				nodeSelect += ", ''"
			}
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO nodes (id, user_id, domain, url, title, is_dead_end, created_at) SELECT %s FROM nodes_old", nodeSelect)); err != nil {
		return err
	}
	// Failure leaves orphan _old tables after commit — log it so the next
	// startup's migration isn't silently confusing.
	if _, err := tx.Exec("DROP TABLE nodes_old"); err != nil {
		log.Printf("topology migration: DROP TABLE nodes_old failed (orphan table kept): %v", err)
	}

	// ── Migrate Edges ────────────────────────────────────────────────────────
	oldEdgeCols, _ := getTableColumns(db, "edges")
	if _, err := tx.Exec("ALTER TABLE edges RENAME TO edges_old"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE edges (
			from_id TEXT,
			to_id TEXT,
			user_id TEXT,
			session_id TEXT,
			action TEXT,
			args TEXT,
			created_at INTEGER
		)`); err != nil {
		return err
	}

	edgeSelect := "from_id, to_id, 'local_user', 'legacy', action, args"
	if oldEdgeCols["created_at"] {
		edgeSelect += ", created_at"
	} else {
		edgeSelect += ", strftime('%s','now')"
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO edges (from_id, to_id, user_id, session_id, action, args, created_at) SELECT %s FROM edges_old", edgeSelect)); err != nil {
		return err
	}
	if _, err := tx.Exec("DROP TABLE edges_old"); err != nil {
		log.Printf("topology migration: DROP TABLE edges_old failed (orphan table kept): %v", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return ensureIndexes(db)
}

func ensureIndexes(db *sql.DB) error {
	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_nodes_user_domain ON nodes(user_id, domain);
	CREATE INDEX IF NOT EXISTS idx_edges_session ON edges(session_id);
	CREATE INDEX IF NOT EXISTS idx_edges_user_created ON edges(user_id, created_at);
	`
	_, err := db.Exec(indexSQL)
	return err
}


func (t *Topology) GetNodeID(url, title string) string {
	h := sha256.New()
	h.Write([]byte(url + title))
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

func (t *Topology) RecordTransition(fromURL, fromTitle, toURL, toTitle, action string, args any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.transitions.Add(1)

	fromID := t.GetNodeID(fromURL, fromTitle)
	toID := t.GetNodeID(toURL, toTitle)
	now := time.Now().Unix()

	getDomain := func(rawURL string) string {
		u, _ := url.Parse(rawURL)
		return u.Host
	}

	// Insert Nodes with user_id context
	if _, err := t.db.Exec("INSERT OR IGNORE INTO nodes (id, user_id, domain, url, title, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		fromID, t.userID, getDomain(fromURL), fromURL, fromTitle, now); err != nil {
		log.Printf("topology: insert from-node failed: %v", err)
	}
	if _, err := t.db.Exec("INSERT OR IGNORE INTO nodes (id, user_id, domain, url, title, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		toID, t.userID, getDomain(toURL), toURL, toTitle, now); err != nil {
		log.Printf("topology: insert to-node failed: %v", err)
	}

	// Insert Edge with session_id
	argsJSON, _ := json.Marshal(args)
	if _, err := t.db.Exec("INSERT INTO edges (from_id, to_id, user_id, session_id, action, args, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		fromID, toID, t.userID, t.sessionID, action, string(argsJSON), now); err != nil {
		log.Printf("topology: insert edge failed: %v", err)
	}

	// Debounced TTL prune: delete edges older than edgeTTL for this user.
	// Runs at most once per pruneInterval to keep RecordTransition cheap.
	if time.Since(t.lastPrune) > pruneInterval {
		cutoff := time.Now().Add(-edgeTTL).Unix()
		if _, err := t.db.Exec("DELETE FROM edges WHERE user_id = ? AND created_at < ?", t.userID, cutoff); err != nil {
			log.Printf("topology: prune old edges failed: %v", err)
		}
		t.lastPrune = time.Now()
	}

	// SaaS Async Reporting could be triggered here
}

func (t *Topology) MarkDeadEnd(urlStr, title string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := t.GetNodeID(urlStr, title)
	// Scope to the current user: id is sha256(url+title), shared across users
	// (PK is (id, user_id)) — without this filter one user's dead-end mark
	// corrupted every other user's node with the same id.
	if _, err := t.db.Exec("UPDATE nodes SET is_dead_end = 1 WHERE id = ? AND user_id = ?", id, t.userID); err != nil {
		log.Printf("topology: mark dead-end failed: %v", err)
	}
}

func (t *Topology) IsDeadEnd(urlStr, title string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	id := t.GetNodeID(urlStr, title)
	var de int
	err := t.db.QueryRow("SELECT is_dead_end FROM nodes WHERE id = ? AND user_id = ?", id, t.userID).Scan(&de)
	if err == nil && de == 1 {
		t.deadEndHits.Add(1)
		return true
	}
	return false
}

func (t *Topology) SaveContentAnchors(urlStr, title string, anchors ContentAnchor) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.anchorsSaved.Add(1)
	id := t.GetNodeID(urlStr, title)
	data, err := json.Marshal(anchors)
	if err != nil {
		return
	}
	if _, err := t.db.Exec("UPDATE nodes SET content_anchors = ? WHERE id = ? AND user_id = ?", string(data), id, t.userID); err != nil {
		log.Printf("topology: save content-anchors failed: %v", err)
	}
}

func (t *Topology) GetContentAnchors(urlStr, title string) (*ContentAnchor, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.anchorsRead.Add(1)
	id := t.GetNodeID(urlStr, title)
	var data string
	err := t.db.QueryRow("SELECT content_anchors FROM nodes WHERE id = ? AND user_id = ?", id, t.userID).Scan(&data)
	if err != nil || data == "" {
		return nil, err
	}
	var anchors ContentAnchor
	if err := json.Unmarshal([]byte(data), &anchors); err != nil {
		return nil, err
	}
	return &anchors, nil
}

func (t *Topology) GetFullMap() (map[string]any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mapQueries.Add(1)

	nodes := make([]Node, 0)
	rows, err := t.db.Query("SELECT id, user_id, domain, url, title, is_dead_end, content_anchors, created_at FROM nodes WHERE user_id = ?", t.userID)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n Node
		var de int
		if err := rows.Scan(&n.ID, &n.UserID, &n.Domain, &n.URL, &n.Title, &de, &n.ContentAnchors, &n.CreatedAt); err != nil {
			continue
		}
		n.IsDeadEnd = (de == 1)
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}

	edges := make([]Edge, 0)
	rows2, err := t.db.Query("SELECT from_id, to_id, user_id, session_id, action, args, created_at FROM edges WHERE user_id = ? ORDER BY created_at DESC LIMIT ?", t.userID, maxMapEdges)
	if err != nil {
		return nil, fmt.Errorf("query edges: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var e Edge
		if err := rows2.Scan(&e.FromID, &e.ToID, &e.UserID, &e.SessionID, &e.Action, &e.Args, &e.CreatedAt); err != nil {
			continue
		}
		edges = append(edges, e)
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}

	return map[string]any{"user_id": t.userID, "nodes": nodes, "edges": edges}, nil
}

func (t *Topology) GetDomainMap(domain string) (map[string]any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mapQueries.Add(1)

	nodes := make([]Node, 0)
	rows, err := t.db.Query("SELECT id, user_id, domain, url, title, is_dead_end, content_anchors, created_at FROM nodes WHERE domain = ? AND user_id = ?", domain, t.userID)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n Node
		var de int
		if err := rows.Scan(&n.ID, &n.UserID, &n.Domain, &n.URL, &n.Title, &de, &n.ContentAnchors, &n.CreatedAt); err != nil {
			continue
		}
		n.IsDeadEnd = (de == 1)
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}

	edges := make([]Edge, 0)
	rows2, err := t.db.Query("SELECT from_id, to_id, user_id, session_id, action, args, created_at FROM edges WHERE user_id = ? AND from_id IN (SELECT id FROM nodes WHERE domain = ?) ORDER BY created_at DESC LIMIT ?", t.userID, domain, maxMapEdges)
	if err != nil {
		return nil, fmt.Errorf("query edges: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var e Edge
		if err := rows2.Scan(&e.FromID, &e.ToID, &e.UserID, &e.SessionID, &e.Action, &e.Args, &e.CreatedAt); err != nil {
			continue
		}
		edges = append(edges, e)
	}
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}

	return map[string]any{"domain": domain, "user_id": t.userID, "nodes": nodes, "edges": edges}, nil
}

func (t *Topology) Close() {
	t.db.Close()
}
