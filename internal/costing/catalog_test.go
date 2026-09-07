package costing

import "testing"

func TestCatalogOrderedWeakToStrong(t *testing.T) {
	rows := Catalog()
	if len(rows) < 5 {
		t.Fatalf("expected catalog rows, got %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Strength < rows[i-1].Strength {
			t.Fatalf("not sorted: %s(%d) before %s(%d)", rows[i-1].Model, rows[i-1].Strength, rows[i].Model, rows[i].Strength)
		}
	}
}
