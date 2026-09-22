package config

import "testing"

func TestLocalEndpointNumCtxRoundTrip(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	ep, err := s.UpsertLocalEndpoint(LocalEndpoint{
		Name: "Ollama LAN", BaseURL: "http://192.168.1.10:11434/v1", Model: "llama3", NumCtx: 16384,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.NumCtx != 16384 {
		t.Fatalf("numCtx=%d want 16384", ep.NumCtx)
	}
	if err := s.SetActiveProvider(MakeLocalProvider(ep.ID)); err != nil {
		t.Fatal(err)
	}
	if got := s.LocalNumCtx(); got != 16384 {
		t.Fatalf("LocalNumCtx=%d want 16384", got)
	}
	got, ok := s.LocalEndpointByID(ep.ID)
	if !ok || got.NumCtx != 16384 {
		t.Fatalf("stored=%+v ok=%v", got, ok)
	}
}

func TestLocalEndpointNumCtxClamped(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	ep, err := s.UpsertLocalEndpoint(LocalEndpoint{Name: "Bad", NumCtx: -1})
	if err != nil {
		t.Fatal(err)
	}
	if ep.NumCtx != 0 {
		t.Fatalf("negative numCtx=%d want 0", ep.NumCtx)
	}
	ep, err = s.UpsertLocalEndpoint(LocalEndpoint{ID: ep.ID, Name: "Bad", NumCtx: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if ep.NumCtx != maxNumCtx {
		t.Fatalf("huge numCtx=%d want %d", ep.NumCtx, maxNumCtx)
	}
}

func TestDuplicateLocalEndpointKeepsNumCtx(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	src, err := s.UpsertLocalEndpoint(LocalEndpoint{Name: "Src", BaseURL: "http://10.0.0.2:11434/v1", Model: "m", NumCtx: 8192})
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.DuplicateLocalEndpoint(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dup.NumCtx != 8192 {
		t.Fatalf("copy numCtx=%d want 8192", dup.NumCtx)
	}
}
