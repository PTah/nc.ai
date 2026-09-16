package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// DefaultLocalEndpointID is the migrated / first built-in local server slot.
const DefaultLocalEndpointID = "default"

// LocalEndpoint is one OpenAI-compatible LAN/local server profile.
type LocalEndpoint struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model,omitempty"`
}

// IsLocalProvider reports whether provider is "local" or "local:<id>".
func IsLocalProvider(provider string) bool {
	p := strings.TrimSpace(strings.ToLower(provider))
	return p == ProviderLocal || strings.HasPrefix(p, ProviderLocal+":")
}

// LocalEndpointID extracts the endpoint slug from "local" / "local:<id>".
func LocalEndpointID(provider string) string {
	p := strings.TrimSpace(strings.ToLower(provider))
	if p == ProviderLocal || p == ProviderLocal+":" {
		return DefaultLocalEndpointID
	}
	if strings.HasPrefix(p, ProviderLocal+":") {
		id := NormalizeEndpointID(strings.TrimPrefix(p, ProviderLocal+":"))
		if id == "" {
			return DefaultLocalEndpointID
		}
		return id
	}
	return DefaultLocalEndpointID
}

// MakeLocalProvider builds "local:<id>".
func MakeLocalProvider(endpointID string) string {
	id := NormalizeEndpointID(endpointID)
	if id == "" {
		id = DefaultLocalEndpointID
	}
	return ProviderLocal + ":" + id
}

// LocalSecretID is the secrets-store key for a local endpoint.
// The default slot keeps legacy id "local" so existing tokens keep working.
func LocalSecretID(endpointID string) string {
	id := NormalizeEndpointID(endpointID)
	if id == "" || id == DefaultLocalEndpointID {
		return "local"
	}
	return ProviderLocal + ":" + id
}

var endpointIDRe = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeEndpointID lowercases and slugifies an endpoint id.
func NormalizeEndpointID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return ""
	}
	id = endpointIDRe.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if len(id) > 48 {
		id = id[:48]
		id = strings.Trim(id, "-")
	}
	return id
}

func slugFromName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' || r == '.' {
			b.WriteByte('-')
		}
	}
	return NormalizeEndpointID(b.String())
}

// ensureLocalEndpointsLocked migrates legacy flat Local* fields into LocalEndpoints.
// Caller must hold s.mu.
func (s *Store) ensureLocalEndpointsLocked() bool {
	changed := false
	if len(s.settings.LocalEndpoints) == 0 {
		ep := LocalEndpoint{
			ID:      DefaultLocalEndpointID,
			Name:    "Local",
			BaseURL: normalizeLocalBaseURL(s.settings.LocalBaseURL),
			Model:   strings.TrimSpace(s.settings.LocalModel),
		}
		s.settings.LocalEndpoints = []LocalEndpoint{ep}
		changed = true
	}
	seen := map[string]bool{}
	out := make([]LocalEndpoint, 0, len(s.settings.LocalEndpoints))
	for i, ep := range s.settings.LocalEndpoints {
		id := NormalizeEndpointID(ep.ID)
		if id == "" {
			id = DefaultLocalEndpointID
			if i > 0 {
				id = fmt.Sprintf("server-%d", i+1)
			}
		}
		for seen[id] {
			id = fmt.Sprintf("%s-%d", id, i+1)
		}
		seen[id] = true
		name := strings.TrimSpace(ep.Name)
		if name == "" {
			name = id
		}
		url := normalizeLocalBaseURL(ep.BaseURL)
		model := strings.TrimSpace(ep.Model)
		norm := LocalEndpoint{ID: id, Name: name, BaseURL: url, Model: model}
		if ep.ID != id || ep.Name != name || ep.BaseURL != url || ep.Model != model {
			changed = true
		}
		out = append(out, norm)
	}
	s.settings.LocalEndpoints = out
	s.syncLegacyLocalFieldsLocked()
	return changed
}

// syncLegacyLocalFieldsLocked mirrors the active (or first) endpoint into flat Local* fields.
func (s *Store) syncLegacyLocalFieldsLocked() {
	ep := s.activeLocalEndpointLocked()
	if ep == nil && len(s.settings.LocalEndpoints) > 0 {
		ep = &s.settings.LocalEndpoints[0]
	}
	if ep == nil {
		s.settings.LocalBaseURL = DefaultLocalBaseURL
		s.settings.LocalModel = ""
		return
	}
	s.settings.LocalBaseURL = ep.BaseURL
	s.settings.LocalModel = ep.Model
}

func (s *Store) activeLocalEndpointLocked() *LocalEndpoint {
	id := LocalEndpointID(s.settings.ActiveProvider)
	for i := range s.settings.LocalEndpoints {
		if s.settings.LocalEndpoints[i].ID == id {
			return &s.settings.LocalEndpoints[i]
		}
	}
	return nil
}

func (s *Store) indexLocalEndpointLocked(id string) int {
	id = NormalizeEndpointID(id)
	for i := range s.settings.LocalEndpoints {
		if s.settings.LocalEndpoints[i].ID == id {
			return i
		}
	}
	return -1
}

// LocalEndpoints returns a copy of configured local servers.
func (s *Store) LocalEndpoints() []LocalEndpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]LocalEndpoint, len(s.settings.LocalEndpoints))
	copy(out, s.settings.LocalEndpoints)
	return out
}

// UpsertLocalEndpoint creates or updates a local server profile.
// Empty id allocates a new slug from name (or "server").
func (s *Store) UpsertLocalEndpoint(ep LocalEndpoint) (LocalEndpoint, error) {
	s.mu.Lock()
	s.ensureLocalEndpointsLocked()
	id := NormalizeEndpointID(ep.ID)
	name := strings.TrimSpace(ep.Name)
	if name == "" {
		name = "Local"
	}
	if id == "" {
		id = slugFromName(name)
		if id == "" {
			id = "server"
		}
		base := id
		n := 2
		for s.indexLocalEndpointLocked(id) >= 0 {
			id = fmt.Sprintf("%s-%d", base, n)
			n++
		}
	}
	url := normalizeLocalBaseURL(ep.BaseURL)
	model := strings.TrimSpace(ep.Model)
	next := LocalEndpoint{ID: id, Name: name, BaseURL: url, Model: model}
	if i := s.indexLocalEndpointLocked(id); i >= 0 {
		if next.Model == "" {
			next.Model = s.settings.LocalEndpoints[i].Model
		}
		s.settings.LocalEndpoints[i] = next
	} else {
		s.settings.LocalEndpoints = append(s.settings.LocalEndpoints, next)
	}
	s.syncLegacyLocalFieldsLocked()
	s.mu.Unlock()
	return next, s.Save()
}

// RemoveLocalEndpoint deletes a profile. Refuses to remove the last one.
// If the active provider pointed at it, switches to the first remaining.
func (s *Store) RemoveLocalEndpoint(id string) error {
	s.mu.Lock()
	s.ensureLocalEndpointsLocked()
	id = NormalizeEndpointID(id)
	i := s.indexLocalEndpointLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return fmt.Errorf("local endpoint %q not found", id)
	}
	if len(s.settings.LocalEndpoints) <= 1 {
		s.mu.Unlock()
		return fmt.Errorf("нельзя удалить последний локальный сервер")
	}
	s.settings.LocalEndpoints = append(s.settings.LocalEndpoints[:i], s.settings.LocalEndpoints[i+1:]...)
	if IsLocalProvider(s.settings.ActiveProvider) && LocalEndpointID(s.settings.ActiveProvider) == id {
		s.settings.ActiveProvider = MakeLocalProvider(s.settings.LocalEndpoints[0].ID)
	}
	s.syncLegacyLocalFieldsLocked()
	secID := LocalSecretID(id)
	_ = s.ensureSecretsLocked()
	if s.sec != nil {
		_ = s.sec.Delete(secID)
		delete(s.keys, secID)
	}
	s.mu.Unlock()
	return s.Save()
}

// DuplicateLocalEndpoint clones an endpoint with a new id/name.
func (s *Store) DuplicateLocalEndpoint(id string) (LocalEndpoint, error) {
	s.mu.Lock()
	s.ensureLocalEndpointsLocked()
	i := s.indexLocalEndpointLocked(id)
	if i < 0 {
		s.mu.Unlock()
		return LocalEndpoint{}, fmt.Errorf("local endpoint %q not found", id)
	}
	src := s.settings.LocalEndpoints[i]
	s.mu.Unlock()
	return s.UpsertLocalEndpoint(LocalEndpoint{
		Name:    src.Name + " copy",
		BaseURL: src.BaseURL,
		Model:   src.Model,
	})
}

// LocalEndpointByID returns one endpoint or ok=false.
func (s *Store) LocalEndpointByID(id string) (LocalEndpoint, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := s.indexLocalEndpointLocked(id)
	if i < 0 {
		return LocalEndpoint{}, false
	}
	return s.settings.LocalEndpoints[i], true
}
