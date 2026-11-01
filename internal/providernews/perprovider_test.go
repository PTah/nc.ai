// Created by nc.ai 1.0.4 on Papasha 2026-11-01 12:30:49
package providernews

import "testing"

// Пятый источник приносит длинную историю: без ограничения на провайдера свежие
// новости остальных вытесняются из дайджеста.
func TestDedupeSortCapsPerProvider(t *testing.T) {
	var items []Item
	for i := 0; i < MaxPerProvider+8; i++ {
		day := 10 + i
		if day > 28 {
			day = 28
		}
		items = append(items, Item{
			ID:       "kimi|" + itoa(day),
			Provider: "Kimi",
			Date:     "2026-10-0" + string(rune('0'+day%10)),
		})
	}
	items = append(items, Item{ID: "ds|1", Provider: "DeepSeek", Date: "2026-09-30"})
	got := dedupeSort(items)
	perProvider := map[string]int{}
	for _, it := range got {
		perProvider[it.Provider]++
	}
	if perProvider["Kimi"] > MaxPerProvider {
		t.Fatalf("Kimi занимает %d записей, лимит %d", perProvider["Kimi"], MaxPerProvider)
	}
	if perProvider["DeepSeek"] != 1 {
		t.Fatalf("запись DeepSeek потерялась: %+v", perProvider)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
