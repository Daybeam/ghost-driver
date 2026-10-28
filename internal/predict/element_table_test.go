package predict

import (
	"strings"
	"testing"
)

func TestElementTable(t *testing.T) {
	et := NewElementTable()
	item1 := et.Add("button", "button", "Submit", "#sub", 10, 20, 100, 30)
	item2 := et.Add("input", "textbox", "Email", "#email", 10, 60, 200, 30)

	if item1.Index != 1 || item2.Index != 2 {
		t.Errorf("got indices %d, %d; want 1, 2", item1.Index, item2.Index)
	}

	gotItem, ok := et.Get(1)
	if !ok || gotItem.Label != "Submit" {
		t.Errorf("et.Get(1) failed or incorrect label: %v", gotItem)
	}

	formatted := et.Format()
	if !strings.Contains(formatted, "[1] button 'Submit'") || !strings.Contains(formatted, "[2] textbox 'Email'") {
		t.Errorf("et.Format() unexpected output:\n%s", formatted)
	}
}
