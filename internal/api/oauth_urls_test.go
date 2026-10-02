package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newOAuthURLRequest builds a request whose URL path is the MCP endpoint, with
// an explicitly set Host and optional forwarded headers / direct TLS.
func newOAuthURLRequest(host string, headers map[string]string, tlsOn bool) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://placeholder.example/api/v1/mcp", nil)
	r.Host = host
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if tlsOn {
		r.TLS = &tls.ConnectionState{}
	}
	return r
}

func TestRequestSchemeAndHost(t *testing.T) {
	const public = "https://aether.example.com"

	tests := []struct {
		name       string
		host       string
		headers    map[string]string
		tlsOn      bool
		publicURL  string
		wantScheme string
		wantHost   string
	}{
		{
			name:       "plain request uses request host",
			host:       "aether.example.com",
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "X-Forwarded-Proto upgrades scheme",
			host:       "aether.example.com",
			headers:    map[string]string{"X-Forwarded-Proto": "https"},
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "direct TLS upgrades scheme",
			host:       "aether.example.com",
			tlsOn:      true,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "X-Forwarded-Host overrides rewritten host",
			host:       "aether-api.aether.svc.cluster.local",
			headers:    map[string]string{"X-Forwarded-Host": "public.example.com"},
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "public.example.com",
		},
		{
			name:       "X-Forwarded-Host chain uses first value",
			host:       "aether-api.aether.svc.cluster.local",
			headers:    map[string]string{"X-Forwarded-Host": "public.example.com, internal.example.com"},
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "public.example.com",
		},
		{
			name: "X-Forwarded-Host and X-Forwarded-Proto combine",
			host: "aether-api.aether.svc.cluster.local",
			headers: map[string]string{
				"X-Forwarded-Host":  "org1.aether.example.com",
				"X-Forwarded-Proto": "https",
			},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "org1.aether.example.com",
		},
		{
			name:       "cluster.local host falls back to public URL host and scheme",
			host:       "aether-api.aether.svc.cluster.local",
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "bare service name falls back to public URL",
			host:       "aether-api",
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "internal host with port falls back to public URL",
			host:       "aether-api:8080",
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			// An edge that terminates TLS can hand off to an inner hop that
			// forwards plaintext (X-Forwarded-Proto: http). The operator's
			// https public URL is authoritative for its own domain, so
			// discovery must not be downgraded (the v0.59.0 regression).
			name:       "public URL scheme wins over X-Forwarded-Proto http",
			host:       "aether-api.aether.svc.cluster.local",
			headers:    map[string]string{"X-Forwarded-Proto": "http"},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "public host with X-Forwarded-Proto http uses public URL scheme",
			host:       "aether.example.com",
			headers:    map[string]string{"X-Forwarded-Proto": "http"},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "org subdomain with X-Forwarded-Proto http uses public URL scheme",
			host:       "org1.aether.example.com",
			headers:    map[string]string{"X-Forwarded-Proto": "http"},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "org1.aether.example.com",
		},
		{
			// Rewriting proxy: the internal Host is replaced by the forwarded
			// host, while the scheme still comes from the public URL instead of
			// the inner plaintext hop.
			name: "rewritten host with X-Forwarded-Host and X-Forwarded-Proto http",
			host: "aether-api.aether.svc.cluster.local",
			headers: map[string]string{
				"X-Forwarded-Host":  "org1.aether.example.com",
				"X-Forwarded-Proto": "http",
			},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "org1.aether.example.com",
		},
		{
			// Host-preserving proxy: a client-supplied X-Forwarded-Host must
			// not steer discovery metadata (phishing / audience poisoning).
			name:       "client-forged X-Forwarded-Host is ignored on external host",
			host:       "aether.example.com",
			headers:    map[string]string{"X-Forwarded-Host": "evil.example"},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "client-forged X-Forwarded-Host is ignored without public URL",
			host:       "aether.example.com",
			headers:    map[string]string{"X-Forwarded-Host": "evil.example"},
			publicURL:  "",
			wantScheme: "http",
			wantHost:   "aether.example.com",
		},
		{
			name:       "public URL scheme wins over direct TLS",
			host:       "aether.example.com",
			tlsOn:      true,
			publicURL:  "http://aether.example.com",
			wantScheme: "http",
			wantHost:   "aether.example.com",
		},
		{
			name:       "X-Forwarded-Proto chain uses first value without public URL",
			host:       "aether.example.com",
			headers:    map[string]string{"X-Forwarded-Proto": "https, http"},
			publicURL:  "",
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			// Loopback / port-forward carve-out: a matching public URL must not
			// rewrite the host or override the request-derived scheme.
			name:       "loopback host with matching public URL keeps request scheme",
			host:       "localhost:8088",
			publicURL:  "https://localhost:8443",
			wantScheme: "http",
			wantHost:   "localhost:8088",
		},
		{
			name:       "loopback host with forwarded https keeps forwarded scheme",
			host:       "localhost:8088",
			headers:    map[string]string{"X-Forwarded-Proto": "https"},
			publicURL:  "https://localhost",
			wantScheme: "https",
			wantHost:   "localhost:8088",
		},
		{
			name:       "IP literal host with matching public URL keeps request scheme",
			host:       "10.0.0.5:8088",
			publicURL:  "https://10.0.0.5",
			wantScheme: "http",
			wantHost:   "10.0.0.5:8088",
		},
		{
			name:       "internal host with empty public URL stays derived",
			host:       "aether-api.aether.svc.cluster.local",
			publicURL:  "",
			wantScheme: "http",
			wantHost:   "aether-api.aether.svc.cluster.local",
		},
		{
			name:       "localhost is never rewritten",
			host:       "localhost:8080",
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "localhost:8080",
		},
		{
			name:       "127.0.0.1 is never rewritten",
			host:       "127.0.0.1:8080",
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "127.0.0.1:8080",
		},
		{
			name:       "IPv6 loopback is never rewritten",
			host:       "[::1]:8080",
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "[::1]:8080",
		},
		{
			name:       "external host is never rewritten even with public URL",
			host:       "other.example.org",
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "other.example.org",
		},
		{
			name:       "X-Forwarded-Host wins over internal-host fallback",
			host:       "aether-api.aether.svc.cluster.local",
			headers:    map[string]string{"X-Forwarded-Host": "org2.aether.example.com"},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "org2.aether.example.com",
		},
		{
			// TLS terminated at a proxy that forwards the host but not the
			// protocol: the public URL's scheme must fill the gap, otherwise
			// https deployments advertise http:// discovery URLs.
			name:       "missing proto with X-Forwarded-Host uses public URL scheme",
			host:       "aether-api.aether.svc.cluster.local",
			headers:    map[string]string{"X-Forwarded-Host": "aether.example.com"},
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com",
		},
		{
			name:       "missing proto on org subdomain uses public URL scheme",
			host:       "org1.aether.example.com",
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "org1.aether.example.com",
		},
		{
			name:       "public URL host match ignores port",
			host:       "aether.example.com:8443",
			publicURL:  public,
			wantScheme: "https",
			wantHost:   "aether.example.com:8443",
		},
		{
			name:       "unrelated forwarded host keeps http",
			host:       "aether-api.aether.svc.cluster.local",
			headers:    map[string]string{"X-Forwarded-Host": "other.example.org"},
			publicURL:  public,
			wantScheme: "http",
			wantHost:   "other.example.org",
		},
		{
			name:       "no public URL keeps request scheme",
			host:       "aether.example.com",
			publicURL:  "",
			wantScheme: "http",
			wantHost:   "aether.example.com",
		},
		{
			name:       "http public URL keeps http",
			host:       "aether.example.com",
			publicURL:  "http://aether.example.com",
			wantScheme: "http",
			wantHost:   "aether.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newOAuthURLRequest(tt.host, tt.headers, tt.tlsOn)
			scheme, host := requestSchemeAndHost(r, tt.publicURL)
			if scheme != tt.wantScheme || host != tt.wantHost {
				t.Fatalf("requestSchemeAndHost = %s://%s, want %s://%s",
					scheme, host, tt.wantScheme, tt.wantHost)
			}
		})
	}
}

func TestCanonicalResourceURIAndOAuthBaseURL(t *testing.T) {
	r := newOAuthURLRequest("aether-api.aether.svc.cluster.local", map[string]string{
		"X-Forwarded-Host":  "org1.aether.example.com",
		"X-Forwarded-Proto": "https",
	}, false)

	if got, want := canonicalResourceURI(r, "https://aether.example.com"), "https://org1.aether.example.com/api/v1/mcp"; got != want {
		t.Fatalf("canonicalResourceURI = %q, want %q", got, want)
	}
	if got, want := oauthBaseURL(r, "https://aether.example.com"), "https://org1.aether.example.com"; got != want {
		t.Fatalf("oauthBaseURL = %q, want %q", got, want)
	}
}

func TestOAuthBaseURLFallsBackToPublicURL(t *testing.T) {
	r := newOAuthURLRequest("aether-api.aether.svc.cluster.local", nil, false)

	if got, want := oauthBaseURL(r, "https://aether.example.com"), "https://aether.example.com"; got != want {
		t.Fatalf("oauthBaseURL = %q, want %q", got, want)
	}
}

func TestMCPUnauthorizedAdvertisesPublicURL(t *testing.T) {
	r := newOAuthURLRequest("aether-api.aether.svc.cluster.local", nil, false)

	t.Run("oauth enabled advertises resource metadata", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeMCPUnauthorized(rec, r, "https://aether.example.com", true, "missing token")

		want := `Bearer realm="aether", resource_metadata="https://aether.example.com/.well-known/oauth-protected-resource"`
		if got := rec.Header().Get("WWW-Authenticate"); got != want {
			t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("oauth disabled emits plain challenge", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeMCPUnauthorized(rec, r, "https://aether.example.com", false, "missing token")

		if got, want := rec.Header().Get("WWW-Authenticate"), `Bearer realm="aether"`; got != want {
			t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
		}
	})
}

func TestIsLoopbackOrIPHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"localhost:8080", true},
		{"127.0.0.1", true},
		{"127.0.0.1:8080", true},
		{"[::1]", true},
		{"[::1]:8080", true},
		{"::1", true},
		{"10.0.0.5", true},
		{"10.0.0.5:443", true},
		{"aether.example.com", false},
		{"aether-api", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isLoopbackOrIPHost(tt.host); got != tt.want {
			t.Errorf("isLoopbackOrIPHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestIsClusterInternalHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"aether-api", true},
		{"aether-api.aether.svc.cluster.local", true},
		{"AETHER-API.AETHER.SVC.CLUSTER.LOCAL:8080", true},
		{"aether-api:8080", true},
		{"aether.example.com", false},
		{"aether.example.com:8080", false},
		{"localhost", false},
		{"localhost:8080", false},
		{"127.0.0.1", false},
		{"127.0.0.1:8080", false},
		{"[::1]", false},
		{"[::1]:8080", false},
		{"10.0.0.5:443", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isClusterInternalHost(tt.host); got != tt.want {
			t.Errorf("isClusterInternalHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}
