package llm

import (
	"net/http"
	"testing"
	"time"
)

// Settings → Network: "auto" (h2, если сервер умеет) и "http11" (h2 выключен)
// должны реально менять транспорт — иначе обрывы длинных потоков не лечатся.
func TestHTTPProtocolSwitchesTransport(t *testing.T) {
	defer SetHTTPProtocol(ProtocolAuto)

	SetHTTPProtocol(ProtocolHTTP11)
	if got := HTTPProtocolMode(); got != ProtocolHTTP11 {
		t.Fatalf("mode = %q, want %q", got, ProtocolHTTP11)
	}
	if tr := currentStream(); tr.ForceAttemptHTTP2 || tr.TLSNextProto == nil {
		t.Fatal("http11: h2 must be disabled on the stream transport")
	}
	if tr := currentStream(); tr.ResponseHeaderTimeout == 0 {
		t.Fatal("stream transport must keep the response-header timeout")
	}
	if tr := currentPlain(); tr.ResponseHeaderTimeout != 0 {
		t.Fatalf("plain ResponseHeaderTimeout = %s, want 0", tr.ResponseHeaderTimeout)
	}

	SetHTTPProtocol(ProtocolAuto)
	if got := HTTPProtocolMode(); got != ProtocolAuto {
		t.Fatalf("mode = %q, want %q", got, ProtocolAuto)
	}
	if tr := currentStream(); !tr.ForceAttemptHTTP2 || tr.TLSNextProto != nil {
		t.Fatal("auto: h2 must be enabled again")
	}
}

func TestHTTP2KeepaliveSetting(t *testing.T) {
	defer func() {
		SetHTTPProtocol(ProtocolAuto)
		SetHTTP2Keepalive(DefaultHTTP2Ping)
	}()

	SetHTTP2Keepalive(45 * time.Second)
	if got := HTTP2Keepalive(); got != 45*time.Second {
		t.Fatalf("keepalive = %s, want 45s", got)
	}
	tr := currentStream()
	if tr.HTTP2 == nil || tr.HTTP2.SendPingTimeout != 45*time.Second {
		t.Fatalf("h2 keepalive not applied: %+v", tr.HTTP2)
	}
	if tr.HTTP2.PingTimeout == 0 {
		t.Fatal("ping timeout must be set (dead stream detection)")
	}

	SetHTTP2Keepalive(0)
	if tr := currentStream(); tr.HTTP2 != nil {
		t.Fatalf("ping off: HTTP2 config must be nil, got %+v", tr.HTTP2)
	}

	// В режиме http/1.1 h2 выключен — keepalive там не при чём.
	SetHTTP2Keepalive(30 * time.Second)
	SetHTTPProtocol(ProtocolHTTP11)
	if tr := currentStream(); tr.HTTP2 != nil {
		t.Fatalf("http11: HTTP2 config must be nil, got %+v", tr.HTTP2)
	}
}

// Делегаты стабильны: клиенты, созданные до смены режима, видят новый транспорт.
func TestTransportDelegatesCurrentMode(t *testing.T) {
	defer SetHTTPProtocol(ProtocolAuto)
	plain, stream := Transport(), StreamTransport()

	SetHTTPProtocol(ProtocolHTTP11)
	if _, ok := plain.(plainRoundTripper); !ok {
		t.Fatal("plain transport must be the stable delegator")
	}
	if _, ok := stream.(streamRoundTripper); !ok {
		t.Fatal("stream transport must be the stable delegator")
	}
	if got := currentPlain(); got.ForceAttemptHTTP2 {
		t.Fatal("delegator must pick up the http11 mode without rebuilding clients")
	}
}

func TestStreamClientUsesSharedTransport(t *testing.T) {
	defer SetHTTPProtocol(ProtocolAuto)

	timed := StreamClient(&http.Client{Timeout: time.Minute})
	if timed.Timeout != 0 {
		t.Fatalf("stream client timeout = %s, want 0", timed.Timeout)
	}
	if _, ok := timed.Transport.(streamRoundTripper); !ok {
		t.Fatal("stream client must use the shared stream transport")
	}
	if _, ok := StreamClient(nil).Transport.(streamRoundTripper); !ok {
		t.Fatal("stream client without base must use the shared stream transport")
	}
}
