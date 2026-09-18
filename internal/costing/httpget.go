package costing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Price endpoints are small public documents, but some of them (openrouter.ai
// behind Cloudflare) answer in a second, then stall past a minute, then reset
// the connection. A single attempt with one 25s deadline turned that into
// "context deadline exceeded" in the chat, so we now split the timeouts and
// retry a couple of times.
const (
	priceAttemptTimeout = 20 * time.Second
	priceAttempts       = 3
	priceRetryPause     = time.Second
)

var priceHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   8 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

// httpStatusError keeps the status code around so retry logic can tell a rate
// limit (worth another try) from a hard 404 (not worth one).
type httpStatusError struct {
	code int
	url  string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d fetching %s", e.code, e.url)
}

// httpGet fetches a small text/JSON document, retrying transient failures.
func httpGet(ctx context.Context, url string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for attempt := 1; attempt <= priceAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := httpGetOnce(ctx, url)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if attempt == priceAttempts || !retryableFetchErr(ctx, err) {
			break
		}
		select {
		case <-time.After(time.Duration(attempt) * priceRetryPause):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func httpGetOnce(ctx context.Context, url string) ([]byte, error) {
	actx, cancel := context.WithTimeout(ctx, priceAttemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NotCursor.ai price-check/1.0")
	req.Header.Set("Accept", "text/html,text/markdown,*/*")
	res, err := priceHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, &httpStatusError{code: res.StatusCode, url: url}
	}
	return data, nil
}

// retryableFetchErr reports whether another attempt may help. The parent
// context decides whether we are allowed to keep trying at all: when the app
// stops, no retry loop should outlive it.
func retryableFetchErr(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var status *httpStatusError
	if errors.As(err, &status) {
		return status.code == http.StatusRequestTimeout ||
			status.code == http.StatusTooManyRequests ||
			status.code == 425 || status.code >= 500
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	// Dial / TLS / read failures: timeouts, "connection reset by peer", ...
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

var reHTTPStatus = regexp.MustCompile(`HTTP (\d{3})`)

// DescribeFetchError turns a network error into something a user can act on
// instead of the raw Go text ("context deadline exceeded").
func DescribeFetchError(errMsg string) string {
	msg := strings.TrimSpace(errMsg)
	if msg == "" {
		return ""
	}
	if m := reHTTPStatus.FindStringSubmatch(msg); len(m) == 2 {
		code, _ := strconv.Atoi(m[1])
		switch {
		case code == http.StatusTooManyRequests:
			return "сервер ответил 429 (слишком много запросов) — попробуем позже"
		case code >= 500:
			return fmt.Sprintf("сервер ответил HTTP %d (сбой на его стороне)", code)
		case code == http.StatusNotFound:
			return "сервер ответил 404 — страница с ценами переехала"
		default:
			return fmt.Sprintf("сервер ответил HTTP %d", code)
		}
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "deadline exceeded"), strings.Contains(low, "timeout"):
		return fmt.Sprintf("нет ответа от хоста (%d попытки по %d с) — проверьте сеть или VPN",
			priceAttempts, int(priceAttemptTimeout.Seconds()))
	case strings.Contains(low, "no such host"):
		return "не разрешается имя хоста — проверьте DNS или VPN"
	case strings.Contains(low, "connection reset"),
		strings.Contains(low, "connection refused"),
		strings.Contains(low, "broken pipe"),
		strings.Contains(low, "unexpected eof"):
		return "соединение обрывается на середине — похоже, сеть или фильтр режет хост"
	default:
		return msg
	}
}
