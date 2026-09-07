package llm

import "encoding/json"

// UnmarshalJSON accepts both DeepSeek flat cache fields and Z.ai
// usage.prompt_tokens_details.cached_tokens.
func (u *Usage) UnmarshalJSON(data []byte) error {
	type alias Usage
	var w struct {
		alias
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*u = Usage(w.alias)
	if u.PromptCacheHitTokens == 0 && w.PromptTokensDetails != nil && w.PromptTokensDetails.CachedTokens > 0 {
		u.PromptCacheHitTokens = w.PromptTokensDetails.CachedTokens
	}
	if u.PromptCacheMissTokens == 0 && u.PromptTokens > 0 {
		miss := u.PromptTokens - u.PromptCacheHitTokens
		if miss < 0 {
			miss = 0
		}
		u.PromptCacheMissTokens = miss
	}
	return nil
}
