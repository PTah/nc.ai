package netx

import "testing"

func TestCheckURLBlocksPrivate(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/secret",
		"http://localhost/x",
		"http://192.168.1.1/",
		"http://10.0.0.5/",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"ftp://example.com/a",
	}
	for _, raw := range blocked {
		if _, err := CheckURL(raw); err == nil {
			t.Fatalf("expected block for %s", raw)
		}
	}
}

func TestCheckURLAllowsPublicHost(t *testing.T) {
	u, err := CheckURL("https://example.com/path?q=1")
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "example.com" {
		t.Fatalf("host=%s", u.Host)
	}
}
