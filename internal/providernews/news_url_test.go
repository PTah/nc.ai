package providernews

import (
	"strings"
	"testing"
)

// Ссылка в новости должна открывать статью, а не корень блога.
func TestOpenRouterBlogLinksToPost(t *testing.T) {
	body := []byte(`<html><body>
<nav><a href="/blog/">Blog</a><a href="/models">Models</a></nav>
<article class="post-card">
  <a class="card" href="/blog/qwen3-max-prime">
    <h2>Qwen3 Max Prime is now available</h2>
  </a>
  <p>Excerpt about the model.</p>
  <span>September 24, 2026</span>
</article>
</body></html>`)

	items := parseOpenRouterBlog(body)
	if len(items) != 1 {
		t.Fatalf("items=%d, want 1: %+v", len(items), items)
	}
	if items[0].URL != "https://openrouter.ai/blog/qwen3-max-prime" {
		t.Fatalf("url=%q", items[0].URL)
	}
	if items[0].Title == "" || items[0].Date != "2026-09-24" {
		t.Errorf("title=%q date=%q", items[0].Title, items[0].Date)
	}
}

// Без ссылки на статью ведём на сам блог (страница, а не сырой JSON).
func TestOpenRouterBlogFallsBackToIndex(t *testing.T) {
	body := []byte(`<html><body>
<article><h2>Some longer headline here</h2><p>x</p><span>September 24, 2026</span></article>
</body></html>`)

	items := parseOpenRouterBlog(body)
	if len(items) != 1 {
		t.Fatalf("items=%d, want 1", len(items))
	}
	if items[0].URL != openRouterBlogURL {
		t.Fatalf("url=%q, want %q", items[0].URL, openRouterBlogURL)
	}
}

// Новости из каталога моделей не имеют права ссылаться на API-эндпоинт или .md-файл:
// именно из-за этого клик открывал сырой JSON/Markdown.
func TestCatalogLinksAreHumanReadable(t *testing.T) {
	for _, src := range catalogSources {
		if src.link == "" {
			t.Errorf("%s: пустая ссылка для новости", src.provider)
		}
		if strings.Contains(src.link, "/api/") {
			t.Errorf("%s: ссылка ведёт на API (%q)", src.provider, src.link)
		}
		if strings.HasSuffix(src.link, ".md") {
			t.Errorf("%s: ссылка ведёт на .md (%q)", src.provider, src.link)
		}
		if !strings.HasPrefix(src.link, "https://") {
			t.Errorf("%s: ссылка не https (%q)", src.provider, src.link)
		}
	}
}

// Ссылки в самих фидах тоже должны быть человеческими страницами.
func TestFeedLinksAreHumanReadable(t *testing.T) {
	for _, u := range []string{deepSeekUpdatesURL, zaiReleaseNotesURL, openRouterBlogURL, qwenBlogRSSURL} {
		if strings.Contains(u, "/api/") || strings.HasSuffix(u, ".md") {
			t.Errorf("feed url=%q — не страница для человека", u)
		}
	}
}

func TestNeedsLinkMigration(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"api-эндпоинт", "https://openrouter.ai/api/v1/models", true},
		{"markdown-файл", "https://docs.z.ai/guides/overview/pricing.md", true},
		{"пустая ссылка", "", true},
		{"страница моделей", "https://openrouter.ai/models", false},
		{"статья блога", "https://openrouter.ai/blog/qwen3-max-prime", false},
	}
	for _, c := range cases {
		snap := Snapshot{Items: []Item{{ID: "x", URL: c.url}}}
		if got := NeedsLinkMigration(snap); got != c.want {
			t.Errorf("%s: NeedsLinkMigration(%q)=%v, want %v", c.name, c.url, got, c.want)
		}
	}
	if NeedsLinkMigration(Snapshot{}) {
		t.Error("пустой кэш не требует миграции ссылок")
	}
}

func TestOpenRouterPostURLResolvesAbsoluteAndRelative(t *testing.T) {
	raw := `<a href="https://openrouter.ai/blog/absolute-post">t</a><h2>Заголовок новости</h2>`
	if got := openRouterPostURL(raw, len(raw)); got != "https://openrouter.ai/blog/absolute-post" {
		t.Errorf("absolute: %q", got)
	}
	raw = `<a href="https://openrouter.ai/blog">Blog</a><h2>Заголовок новости</h2>`
	if got := openRouterPostURL(raw, len(raw)); got != openRouterBlogURL {
		t.Errorf("индекс блога не должен подставляться как статья: %q", got)
	}
}
