//go:build lite
package browser

type Node struct {
	ID        string `json:"id"`
	Domain    string `json:"domain"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	IsDeadEnd bool   `json:"is_dead_end"`
}

type Edge struct {
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Action string `json:"action"`
	Args   string `json:"args"`
}

type Topology struct{}

func NewTopology(dbPath string, userID string) (*Topology, error) { return &Topology{}, nil }
func (t *Topology) GetNodeID(url, title string) string { return "" }
func (t *Topology) RecordTransition(fromURL, fromTitle, toURL, toTitle, action string, args any) {}
func (t *Topology) MarkDeadEnd(urlStr, title string) {}
func (t *Topology) IsDeadEnd(urlStr, title string) bool { return false }
func (t *Topology) SaveContentAnchors(urlStr, title string, anchors ContentAnchor) {}
func (t *Topology) GetContentAnchors(urlStr, title string) (*ContentAnchor, error) { return nil, nil }
func (t *Topology) GetFullMap() (map[string]any, error) { return map[string]any{}, nil }
func (t *Topology) GetDomainMap(domain string) (map[string]any, error) { return map[string]any{}, nil }
func (t *Topology) Close() {}

type TopologyStats struct {
	DeadEndHits  int64 `json:"dead_end_hits"`
	MapQueries   int64 `json:"map_queries"`
	Transitions  int64 `json:"transitions"`
	AnchorsSaved int64 `json:"anchors_saved"`
	AnchorsRead  int64 `json:"anchors_read"`
}

func (t *Topology) Stats() TopologyStats { return TopologyStats{} }
