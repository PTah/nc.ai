package llm

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPProtocol — протокол, которым ходим к провайдерам (Settings → Network).
type HTTPProtocol string

const (
	// ProtocolAuto — ALPN-переговоры: HTTP/2, если сервер его поддерживает
	// (поведение по умолчанию).
	ProtocolAuto HTTPProtocol = "auto"
	// ProtocolHTTP11 — только HTTP/1.1: h2 выключен. Лечит обрывы длинных
	// SSE-потоков, когда CDN/NAT/DPI рвёт мультиплексированное h2-соединение
	// («connection reset by peer» в середине ответа).
	ProtocolHTTP11 HTTPProtocol = "http11"
)

// HTTP/2 keepalive: h2-PING после стольких секунд тишины (никаких кадров от
// сервера) и сколько ждём ответ на PING. Держит соединение живым на стороне
// NAT/роутера/DPI, но лечит только участок клиент↔edge — если поток рвёт сам
// origin (CloudFront читает origin с таймаутом), поможет смена модели/маршрута.
const (
	DefaultHTTP2Ping = 20 * time.Second
	pingWaitTimeout  = 10 * time.Second
)

var (
	transportMu     sync.RWMutex
	protoMode       = ProtocolAuto
	pingInterval    = DefaultHTTP2Ping
	plainTransport  = newProviderTransport(ProtocolAuto, false)
	streamTransport = newProviderTransport(ProtocolAuto, true)
)

// SetHTTPProtocol переключает протокол для всех запросов к провайдерам.
// Вызывается на старте приложения и при сохранении Settings.
func SetHTTPProtocol(mode HTTPProtocol) {
	transportMu.Lock()
	defer transportMu.Unlock()
	mode = normalizeProtocol(mode)
	if mode == protoMode && plainTransport != nil && streamTransport != nil {
		return
	}
	protoMode = mode
	rebuildTransportsLocked()
}

// SetHTTP2Keepalive включает/выключает h2-PING (0 — выключить health-check).
// PING уходит только когда от сервера нет вообще никаких кадров pingInterval;
// при живом потоке (DATA/окна) он не мешает.
func SetHTTP2Keepalive(d time.Duration) {
	transportMu.Lock()
	defer transportMu.Unlock()
	if d < 0 {
		d = 0
	}
	if d == pingInterval {
		return
	}
	pingInterval = d
	rebuildTransportsLocked()
}

// HTTP2Keepalive возвращает текущий интервал PING (0 = выключено).
func HTTP2Keepalive() time.Duration {
	transportMu.RLock()
	defer transportMu.RUnlock()
	return pingInterval
}

func rebuildTransportsLocked() {
	plainTransport = newProviderTransport(protoMode, false)
	streamTransport = newProviderTransport(protoMode, true)
}

// HTTPProtocolMode возвращает активный режим (для UI и логов).
func HTTPProtocolMode() HTTPProtocol {
	transportMu.RLock()
	defer transportMu.RUnlock()
	return protoMode
}

// Transport — RoundTripper для обычных (не потоковых) запросов к провайдерам.
// Возвращается стабильный делегат, который каждый раз берёт транспорт текущего
// режима: смена протокола в Settings применяется к уже созданным клиентам, их
// не нужно пересоздавать.
func Transport() http.RoundTripper { return plainRoundTripper{} }

// StreamTransport — RoundTripper для SSE: общий Timeout снят (поток живёт
// долго), но заголовки ответа ждём ограниченно, чтобы мёртвый хост отваливался
// сам, а не висел до stall-watchdog.
func StreamTransport() http.RoundTripper { return streamRoundTripper{} }

type plainRoundTripper struct{}

func (plainRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return currentPlain().RoundTrip(req)
}

func (plainRoundTripper) CloseIdleConnections() { currentPlain().CloseIdleConnections() }

type streamRoundTripper struct{}

func (streamRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return currentStream().RoundTrip(req)
}

func (streamRoundTripper) CloseIdleConnections() { currentStream().CloseIdleConnections() }

func currentPlain() *http.Transport {
	transportMu.RLock()
	defer transportMu.RUnlock()
	return plainTransport
}

func currentStream() *http.Transport {
	transportMu.RLock()
	defer transportMu.RUnlock()
	return streamTransport
}

// newProviderTransport собирает транспорт под выбранный протокол.
func newProviderTransport(mode HTTPProtocol, stream bool) *http.Transport {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     mode != ProtocolHTTP11,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if mode == ProtocolHTTP11 {
		// Пустая карта TLSNextProto выключает HTTP/2 поверх TLS — так же, как
		// в internal/update для api.github.com.
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	} else if pingInterval > 0 {
		// H2 keepalive: health-check PING, если от сервера нет кадров вообще
		// (типичная пауза «модель думает» перед следующим чанком).
		tr.HTTP2 = &http.HTTP2Config{
			SendPingTimeout: pingInterval,
			PingTimeout:     pingWaitTimeout,
		}
	}
	if stream {
		tr.ResponseHeaderTimeout = 90 * time.Second
	}
	return tr
}

func normalizeProtocol(mode HTTPProtocol) HTTPProtocol {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case string(ProtocolHTTP11), "http1.1", "http/1.1", "h1", "http1":
		return ProtocolHTTP11
	default:
		return ProtocolAuto
	}
}
