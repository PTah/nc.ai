// Created by nc.ai 0.9.0 on new-admin-pc 2026-10-28 10:27:25
package tools

import (
	"context"
	"fmt"
	"strings"

	"notcursor.ai/app/internal/netx"
)

// Web providers for web_search / fetch_url (Settings → Agent).
const (
	// WebProviderAuto uses the DeepSeek Search/Fetch API when a DeepSeek key is
	// configured and falls back to DuckDuckGo otherwise.
	WebProviderAuto = "auto"
	// WebProviderDeepSeek forces the DeepSeek API (falls back on request errors).
	WebProviderDeepSeek = "deepseek"
	// WebProviderDuckDuckGo scrapes DuckDuckGo and GETs pages directly.
	WebProviderDuckDuckGo = "duckduckgo"
)

// webProvider resolves the effective provider for this run. DuckDuckGo is the
// default on purpose: the DeepSeek Search/Fetch API is billed per request, so it
// is only used when the user explicitly picked it (see docs/exchange-protocols/deepseek.md §14).
func (r *Registry) webProvider() string {
	switch strings.ToLower(strings.TrimSpace(r.WebTools)) {
	case WebProviderDeepSeek:
		return WebProviderDeepSeek
	case WebProviderAuto:
		if strings.TrimSpace(r.DeepSeekKey) != "" {
			return WebProviderDeepSeek
		}
	}
	return WebProviderDuckDuckGo
}

// webSearch runs web_search through the configured provider. The DeepSeek API
// is tried first; any request error (402 no balance, 403 blocked, 429 QPS, 5xx,
// network) falls back to DuckDuckGo instead of failing the step.
func (r *Registry) webSearch(ctx context.Context, query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", fmt.Errorf("query is empty")
	}
	if r.webProvider() != WebProviderDeepSeek {
		out, err := netx.SearchDuckDuckGo(q)
		return wrapUntrusted(out), err
	}
	if strings.TrimSpace(r.DeepSeekKey) == "" {
		return "", fmt.Errorf("веб-поиск DeepSeek: не задан ключ DeepSeek (Settings → Providers); переключите «Веб-поиск и страницы» на DuckDuckGo")
	}
	res, err := netx.DeepSearch(ctx, r.DeepSeekKey, q, netx.DeepSearchOptions{Count: 8})
	if err == nil {
		return wrapUntrusted(netx.FormatDeepSearch(q, res)), nil
	}
	out, ddgErr := netx.SearchDuckDuckGo(q)
	if ddgErr != nil {
		return "", fmt.Errorf("deepseek search: %v; duckduckgo: %v", err, ddgErr)
	}
	return wrapUntrusted(out) + "\n(поиск DeepSeek недоступен: " + err.Error() + " — показаны результаты DuckDuckGo)", nil
}

// webFetch loads one page: DeepSeek Fetch returns clean Markdown, the direct
// GET is the fallback (and the whole path when DuckDuckGo mode is selected).
func (r *Registry) webFetch(ctx context.Context, rawURL string) (string, error) {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return "", fmt.Errorf("url is empty")
	}
	if r.webProvider() != WebProviderDeepSeek {
		out, err := netx.FetchURL(u)
		return wrapUntrusted(out), err
	}
	if strings.TrimSpace(r.DeepSeekKey) == "" {
		return "", fmt.Errorf("загрузка страниц DeepSeek: не задан ключ DeepSeek (Settings → Providers); переключите «Веб-поиск и страницы» на DuckDuckGo")
	}
	page, err := netx.DeepFetch(ctx, r.DeepSeekKey, u)
	if err == nil {
		return wrapUntrusted(netx.FormatDeepPage(u, page)), nil
	}
	out, directErr := netx.FetchURL(u)
	if directErr != nil {
		return "", fmt.Errorf("deepseek fetch: %v; direct fetch: %v", err, directErr)
	}
	return wrapUntrusted(out) + "\n(fetch DeepSeek недоступен: " + err.Error() + " — страница скачана напрямую)", nil
}
