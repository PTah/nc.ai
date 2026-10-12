// Created by nc.ai 0.8.5.2 on macbook 2026-10-12 20:42:38
package update

import (
	"context"
	"os"
	"testing"
)

// Живая проверка зеркала (по умолчанию пропускается):
//
//	NC_LIVE_MIRROR=http://192.168.x.x:3000/PapaTramp/nc.ai go test ./internal/update/ -run TestLiveMirror -v
func TestLiveMirrorCheck(t *testing.T) {
	base := os.Getenv("NC_LIVE_MIRROR")
	if base == "" {
		t.Skip("NC_LIVE_MIRROR не задан")
	}
	defer ConfigureMirror(nil)
	ConfigureMirror(&Mirror{BaseURL: base})

	info, err := Check(context.Background(), "0.8.5.2", "darwin", "arm64")
	if err != nil {
		t.Fatalf("Check через зеркало: %v", err)
	}
	t.Logf("source=%s latest=%s asset=%s size=%d url=%s available=%t",
		info.Source, info.Latest, info.AssetName, info.AssetSize, info.AssetURL, info.Available)
	if info.Source != SourceMirror {
		t.Fatalf("источник = %q, хотели %q", info.Source, SourceMirror)
	}
	if !info.AssetFound {
		t.Fatal("ассет для darwin/arm64 не найден в релизе зеркала")
	}
}
