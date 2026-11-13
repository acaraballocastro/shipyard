package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAccess struct {
	key  *rsa.PrivateKey
	srv  *httptest.Server
	team string
}

func newFakeAccess(t *testing.T) *fakeAccess {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAccess{key: k}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cdn-cgi/access/certs" {
			http.NotFound(w, r)
			return
		}
		b64 := base64.RawURLEncoding.EncodeToString
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1", "kty": "RSA",
			"n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes()),
		}}})
	}))
	t.Cleanup(f.srv.Close)
	f.team = f.srv.URL
	return f
}

func (f *fakeAccess) verifier() *AccessVerifier {
	return &AccessVerifier{TeamDomain: f.team, Audience: "aud-1"}
}

func (f *fakeAccess) token(t *testing.T, claims map[string]any) string {
	b64 := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	base := map[string]any{"iss": f.team, "aud": []string{"aud-1"}, "exp": time.Now().Add(time.Hour).Unix()}
	for k, v := range claims {
		base[k] = v
	}
	signing := b64(map[string]string{"alg": "RS256", "kid": "k1"}) + "." + b64(base)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func cfGet(t *testing.T, url, jwt, token string) int {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set(cfConnectingIP, "203.0.113.7")
	if jwt != "" {
		req.Header.Set(accessJWTHeader, jwt)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestAccessRequiredThroughCloudflare(t *testing.T) {
	fa := newFakeAccess(t)
	s := newServer(t, reg{"local": fakeBackend{}}, func(o *Options) { o.Access = fa.verifier() })
	u := s.URL + "/api/v1/servers"
	good := fa.token(t, map[string]any{"email": "me@example.com"})

	cases := []struct {
		name       string
		jwt, token string
		want       int
	}{
		{"no access token", "", tok, 403},
		{"forged signature", good[:len(good)-4] + "AAAA", tok, 403},
		{"wrong audience", fa.token(t, map[string]any{"aud": "other"}), tok, 403},
		{"expired", fa.token(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}), tok, 403},
		{"wrong issuer", fa.token(t, map[string]any{"iss": "https://evil.cloudflareaccess.com"}), tok, 403},
		{"access ok but no shipyard token (two layers)", good, "", 401},
		{"both layers", good, tok, 200},
	}
	for _, c := range cases {
		if got := cfGet(t, u, c.jwt, c.token); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
	// LAN / SSH tunnel: no Cloudflare header, token alone is enough.
	if r, _ := get(t, u, tok); r.StatusCode != 200 {
		t.Errorf("LAN request: %d", r.StatusCode)
	}
}

func TestAccessLogin(t *testing.T) {
	fa := newFakeAccess(t)
	s := newServer(t, reg{"local": fakeBackend{}}, func(o *Options) {
		o.Access, o.AccessLogin, o.AllowedEmails = fa.verifier(), true, []string{"Me@Example.com"}
	})
	u := s.URL + "/api/v1/servers"
	if got := cfGet(t, u, fa.token(t, map[string]any{"email": "me@example.com"}), ""); got != 200 {
		t.Errorf("allowed user without token: %d", got)
	}
	if got := cfGet(t, u, fa.token(t, map[string]any{"email": "stranger@example.com"}), ""); got != 401 {
		t.Errorf("user not in allow list: %d", got)
	}
	if got := cfGet(t, u, fa.token(t, map[string]any{"common_name": "svc.access"}), ""); got != 401 {
		t.Errorf("service token must not count as login: %d", got)
	}

	req, _ := http.NewRequest("GET", s.URL+"/api/v1/session", nil)
	req.Header.Set(cfConnectingIP, "203.0.113.7")
	req.Header.Set(accessJWTHeader, fa.token(t, map[string]any{"email": "me@example.com"}))
	resp, _ := http.DefaultClient.Do(req)
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body["email"] != "me@example.com" || body["access"] != true {
		t.Errorf("session: %v", body)
	}
}

func TestNewAccessVerifierNormalizesTeam(t *testing.T) {
	for _, in := range []string{"myteam", "myteam.cloudflareaccess.com", "https://myteam.cloudflareaccess.com/"} {
		if v := NewAccessVerifier(in, " aud "); v.TeamDomain != "https://myteam.cloudflareaccess.com" || v.Audience != "aud" {
			t.Errorf("%q -> %q %q", in, v.TeamDomain, v.Audience)
		}
	}
	if !strings.HasPrefix(NewAccessVerifier("x", "a").TeamDomain, "https://") {
		t.Error("scheme")
	}
}
