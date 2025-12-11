package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// loggingTransport wraps a RoundTripper to show explicit usage of Transport.
type loggingTransport struct {
	base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt := t.base
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(req)
}

type dialOverrideKey struct{}

func withDialOverride(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, dialOverrideKey{}, addr)
}

func dialOverrideFromContext(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(dialOverrideKey{}).(string)
	return val, ok
}

var (
	defaultDialer   = &net.Dialer{Timeout: 500 * time.Millisecond}
	sharedTransport = &http.Transport{
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if override, ok := dialOverrideFromContext(ctx); ok {
				addr = override
			}
			return defaultDialer.DialContext(ctx, network, addr)
		},
	}
	sharedRoundTripper = &loggingTransport{base: sharedTransport}
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <url> [ip]\n", os.Args[0])
		os.Exit(1)
	}

	urlStr := os.Args[1]
	var ip string
	if len(os.Args) == 3 {
		ip = os.Args[2]
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		log.Fatalf("parsing url failed: %v", err)
	}
	host := parsedURL.Hostname()
	if host == "" {
		log.Fatalf("url missing host: %q", urlStr)
	}
	port := pickPort(parsedURL)

	dialHost := host
	if ip != "" {
		dialHost = ip
	}
	dialAddr := net.JoinHostPort(dialHost, port)

	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		log.Fatalf("building request failed: %v", err)
	}
	// Keep TLS hostname validation intact by preserving the URL host while overriding the dial target when provided.
	if ip != "" {
		req = req.WithContext(withDialOverride(req.Context(), dialAddr))
	}

	resp, err := sharedRoundTripper.RoundTrip(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	defer sharedTransport.CloseIdleConnections()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("read failed: %v", err)
	}

	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Protocol: %s\n", resp.Proto)
	fmt.Printf("Headers:\n")
	for k, vals := range resp.Header {
		for _, v := range vals {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
	fmt.Printf("Body length: %d bytes\n", len(body))
}

func pickPort(parsedURL *url.URL) string {
	port := parsedURL.Port()
	if port != "" {
		return port
	}
	switch strings.ToLower(parsedURL.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		log.Fatalf("unknown url scheme %q", parsedURL.Scheme)
	}
	return ""
}
