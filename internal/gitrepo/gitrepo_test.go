// Created by nc.ai 1.2.5 on macbook 2026-11-12 20:30:01
// Created by nc.ai 1.2.6 on macbook 2026-11-12
package gitrepo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseKind(t *testing.T) {
	cases := map[string]Kind{
		"":                 KindNone,
		"none":             KindNone,
		"локально":         KindNone,
		"github":           KindGitHub,
		"GitHub":           KindGitHub,
		"гитхаб":           KindGitHub,
		"gitea":            KindGiteaHome,
		"home":             KindGiteaHome,
		"домашний":         KindGiteaHome,
		"gitea-home":       KindGiteaHome,
		"kalinamall":       KindGiteaKalinamall,
		"gitea-kalinamall": KindGiteaKalinamall,
	}
	for in, want := range cases {
		got, err := ParseKind(in)
		if err != nil {
			t.Errorf("ParseKind(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseKind(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseKind("bitbucket"); err == nil {
		t.Error("ожидалась ошибка для неизвестного хостинга")
	}
}

func TestCreateGitea(t *testing.T) {
	var gotPath, gotToken string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"clone_url":"https://git.example.com/o/n.git","ssh_url":"ssh://git@git.example.com:2222/o/n.git","html_url":"https://git.example.com/o/n"}`)
	}))
	defer srv.Close()

	c := NewClient()
	res, err := c.Create(t.Context(), Request{
		Kind: KindGiteaHome, BaseURL: srv.URL, Token: "tok", Owner: "PapaTramp",
		Name: "n", Description: "d", Private: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotPath != "/api/v1/orgs/PapaTramp/repos" {
		t.Errorf("path = %q", gotPath)
	}
	if gotToken != "token tok" {
		t.Errorf("Authorization = %q, want token tok", gotToken)
	}
	if gotBody["name"] != "n" || gotBody["private"] != true || gotBody["auto_init"] != false {
		t.Errorf("body = %v", gotBody)
	}
	if !strings.Contains(res.SSHURL, "2222") {
		t.Errorf("ssh_url потерян: %+v", res)
	}
}

func TestCreateGiteaPersonalAccount(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"ssh_url":"ssh://git@h/o/n.git"}`)
	}))
	defer srv.Close()

	if _, err := NewClient().Create(t.Context(), Request{
		Kind: KindGiteaKalinamall, BaseURL: srv.URL, Token: "tok", Name: "n",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if gotPath != "/api/v1/user/repos" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestCreateGiteaConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"repository already exists"}`)
	}))
	defer srv.Close()

	_, err := NewClient().Create(t.Context(), Request{Kind: KindGiteaHome, BaseURL: srv.URL, Token: "t", Name: "n"})
	if err == nil || !strings.Contains(err.Error(), "уже существует") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateWithoutToken(t *testing.T) {
	_, err := NewClient().Create(t.Context(), Request{Kind: KindGitHub, Name: "n"})
	if err == nil || !strings.Contains(err.Error(), "токен") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateGiteaWithoutBaseURL(t *testing.T) {
	_, err := NewClient().Create(t.Context(), Request{Kind: KindGiteaHome, Token: "t", Name: "n"})
	if err == nil || !strings.Contains(err.Error(), "адрес Gitea") {
		t.Fatalf("err = %v", err)
	}
}
