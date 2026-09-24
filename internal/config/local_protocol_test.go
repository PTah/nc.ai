package config

import "testing"

func TestEndpointProtocolNormalization(t *testing.T) {
	cases := map[string]string{
		"":                   "",
		"openai":             "",
		"openai-chat":        "",
		"anthropic":          ProtocolAnthropic,
		" Anthropic ":        ProtocolAnthropic,
		"claude":             ProtocolAnthropic,
		"messages":           ProtocolAnthropic,
		"anthropic-messages": ProtocolAnthropic,
		"что-то своё":        "",
	}
	for in, want := range cases {
		if got := EndpointProtocol(in); got != want {
			t.Errorf("EndpointProtocol(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestEndpointReasoningEffortNormalization(t *testing.T) {
	cases := map[string]string{
		"":        "",
		"off":     "",
		"HIGH":    "high",
		" low ":   "low",
		"medium":  "medium",
		"extreme": "",
	}
	for in, want := range cases {
		if got := EndpointReasoningEffort(in); got != want {
			t.Errorf("EndpointReasoningEffort(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestUpsertEndpointProtocolAndReasoning(t *testing.T) {
	s := NewStoreForTest(t.TempDir())

	// Anthropic-профиль без Base URL получает адрес по умолчанию, а не Ollama.
	ep, err := s.UpsertLocalEndpoint(LocalEndpoint{
		Name: "Atria Claude", Protocol: "anthropic", Model: "claude-sonnet-4",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if ep.BaseURL != DefaultAnthropicBaseURL {
		t.Errorf("baseUrl=%q, want %q", ep.BaseURL, DefaultAnthropicBaseURL)
	}
	if ep.Protocol != ProtocolAnthropic {
		t.Errorf("protocol=%q", ep.Protocol)
	}

	ep, err = s.UpsertLocalEndpoint(LocalEndpoint{
		ID: ep.ID, Name: ep.Name, BaseURL: "https://api.selora.ai/v1",
		Model: ep.Model, Protocol: "claude", ReasoningEffort: "HIGH",
	})
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if ep.ReasoningEffort != "high" {
		t.Errorf("reasoning=%q", ep.ReasoningEffort)
	}
	// Модель не потерялась при обновлении с пустым Model.
	ep, err = s.UpsertLocalEndpoint(LocalEndpoint{
		ID: ep.ID, Name: ep.Name, BaseURL: ep.BaseURL,
		Protocol: ep.Protocol, ReasoningEffort: ep.ReasoningEffort,
	})
	if err != nil {
		t.Fatalf("upsert 3: %v", err)
	}
	if ep.Model != "claude-sonnet-4" {
		t.Errorf("model=%q — сохранённая модель потерялась", ep.Model)
	}

	dup, err := s.DuplicateLocalEndpoint(ep.ID)
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if dup.Protocol != ProtocolAnthropic || dup.ReasoningEffort != "high" {
		t.Errorf("дубликат потерял настройки: %+v", dup)
	}

	// Настройки читаются с диска уже с протоколом.
	stored, ok := s.LocalEndpointByID(ep.ID)
	if !ok || stored.Protocol != ProtocolAnthropic {
		t.Fatalf("stored=%+v ok=%v", stored, ok)
	}
}
