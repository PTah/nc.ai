// Created by nc.ai 1.2.5 on macbook 2026-11-12 20:43:44
// Created by nc.ai 1.2.7 on macbook 2026-11-12
package gitrepo

import "testing"

// Адреса нового репозитория строятся из настроек хостинга: ssh — по уже
// настроенным на машине ключам, https — как запасной вариант.
func TestHostRepoURLs(t *testing.T) {
	cases := []struct {
		name    string
		host    Host
		owner   string
		wantSSH string
		wantHTT string
	}{
		{
			name:    "github",
			host:    Host{Name: "github", Kind: "github", Owner: "PTah"},
			wantSSH: "git@github.com:PTah/repo.git",
			wantHTT: "https://github.com/PTah/repo.git",
		},
		{
			name:    "gitea ssh scp-подобный",
			host:    Host{Name: "Home", Kind: "gitea", BaseURL: "https://git.example.com", SSHBase: "git.example.com:2222", Owner: "PapaTramp"},
			wantSSH: "ssh://git@git.example.com:2222/PapaTramp/repo.git",
			wantHTT: "https://git.example.com/PapaTramp/repo.git",
		},
		{
			name:    "gitea ssh url",
			host:    Host{Name: "kal", Kind: "gitea", BaseURL: "https://git.kal.example/", SSHBase: "ssh://git@git.kal.example:2222"},
			owner:   "PapaTramp",
			wantSSH: "ssh://git@git.kal.example:2222/PapaTramp/repo.git",
			wantHTT: "https://git.kal.example/PapaTramp/repo.git",
		},
	}
	for _, c := range cases {
		ssh, https := c.host.RepoURLs(c.owner, "repo")
		if ssh != c.wantSSH {
			t.Errorf("%s: ssh = %q, want %q", c.name, ssh, c.wantSSH)
		}
		if https != c.wantHTT {
			t.Errorf("%s: https = %q, want %q", c.name, https, c.wantHTT)
		}
	}
}

// Владелец из запроса важнее владельца по умолчанию в настройках хостинга.
func TestHostOwnerOr(t *testing.T) {
	h := Host{Owner: "PapaTramp"}
	if got := h.OwnerOr(""); got != "PapaTramp" {
		t.Fatalf("OwnerOr(\"\") = %q", got)
	}
	if got := h.OwnerOr("Other"); got != "Other" {
		t.Fatalf("OwnerOr(Other) = %q", got)
	}
	if ssh, _ := h.RepoURLs("Other", "r"); ssh != "" {
		t.Fatalf("ssh = %q", ssh)
	}
}

func TestHostKindOf(t *testing.T) {
	if (Host{Kind: "github"}).KindOf() != KindGitHub {
		t.Fatal("github не распознан")
	}
	if (Host{Kind: "gitea"}).KindOf() != KindGiteaHome {
		t.Fatal("gitea не распознан")
	}
}
