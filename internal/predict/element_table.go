package predict

import (
	"fmt"
	"strings"
)

// ElementItem represents a single interactive DOM element in the element table.
type ElementItem struct {
	Index    int     `json:"index"`
	Tag      string  `json:"tag"`
	Role     string  `json:"role,omitempty"`
	Label    string  `json:"label"`
	Selector string  `json:"selector,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width,omitempty"`
	Height   float64 `json:"height,omitempty"`
}

// ElementTable holds a map of 1-based indexed elements.
type ElementTable struct {
	Items map[int]ElementItem `json:"items"`
}

// NewElementTable initializes an empty ElementTable.
func NewElementTable() *ElementTable {
	return &ElementTable{
		Items: make(map[int]ElementItem),
	}
}

// Add adds a new element item and sets its 1-based index.
func (et *ElementTable) Add(tag, role, label, selector string, x, y, width, height float64) ElementItem {
	idx := len(et.Items) + 1
	item := ElementItem{
		Index:    idx,
		Tag:      tag,
		Role:     role,
		Label:    strings.TrimSpace(label),
		Selector: selector,
		X:        x,
		Y:        y,
		Width:    width,
		Height:   height,
	}
	et.Items[idx] = item
	return item
}

// Get retrieves an item by its 1-based index.
func (et *ElementTable) Get(idx int) (ElementItem, bool) {
	item, ok := et.Items[idx]
	return item, ok
}

// Format returns a concise, token-efficient string representation of the element table.
func (et *ElementTable) Format() string {
	var sb strings.Builder
	for i := 1; i <= len(et.Items); i++ {
		item := et.Items[i]
		tagOrRole := item.Role
		if tagOrRole == "" {
			tagOrRole = item.Tag
		}
		sb.WriteString(fmt.Sprintf("[%d] %s '%s' (x=%.0f, y=%.0f)\n", item.Index, tagOrRole, item.Label, item.X, item.Y))
	}
	return strings.TrimRight(sb.String(), "\n")
}
