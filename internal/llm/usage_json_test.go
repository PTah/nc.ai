package llm

import (
	"encoding/json"
	"testing"
)

func TestUsageUnmarshalZaiCachedTokens(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 1200,
		"completion_tokens": 300,
		"total_tokens": 1500,
		"prompt_tokens_details": { "cached_tokens": 800 }
	}`)
	var u Usage
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	if u.PromptTokens != 1200 || u.CompletionTokens != 300 {
		t.Fatalf("tokens=%+v", u)
	}
	if u.PromptCacheHitTokens != 800 {
		t.Fatalf("hit=%d", u.PromptCacheHitTokens)
	}
	if u.PromptCacheMissTokens != 400 {
		t.Fatalf("miss=%d", u.PromptCacheMissTokens)
	}
}

func TestUsageUnmarshalDeepSeekFlat(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 100,
		"completion_tokens": 50,
		"prompt_cache_hit_tokens": 80,
		"prompt_cache_miss_tokens": 20
	}`)
	var u Usage
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	if u.PromptCacheHitTokens != 80 || u.PromptCacheMissTokens != 20 {
		t.Fatalf("%+v", u)
	}
}
