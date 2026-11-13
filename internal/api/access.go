package api

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Cloudflare Access puts a signed JWT on every request it lets through.
// Verifying it means a request that came through Cloudflare was really
// approved by your Access policy, even if a hostname was left without one.
const (
	accessJWTHeader = "Cf-Access-Jwt-Assertion"
	accessCookie    = "CF_Authorization"
	// cfConnectingIP is set by Cloudflare on every proxied or tunneled
	// request and cannot be removed by the client: its presence means
	// "this came through Cloudflare".
	cfConnectingIP = "Cf-Connecting-Ip"
)

// Identity is who Cloudflare Access authenticated.
type Identity struct {
	Email string // users (Google, GitHub, one-time PIN…)
	// CommonName is set instead for service tokens (e.g. shipyard ui).
	CommonName string
}

// AccessVerifier checks Cloudflare Access JWTs for one Access application.
type AccessVerifier struct {
	// TeamDomain is "https://<team>.cloudflareaccess.com".
	TeamDomain string
	// Audience is the application's AUD tag.
	Audience string
	HTTP     *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	now       func() time.Time // tests
}

// NewAccessVerifier normalizes team ("myteam", "myteam.cloudflareaccess.com"
// or the full URL) and returns a verifier.
func NewAccessVerifier(team, audience string) *AccessVerifier {
	team = strings.TrimRight(strings.TrimSpace(team), "/")
	team = strings.TrimPrefix(strings.TrimPrefix(team, "https://"), "http://")
	if !strings.Contains(team, ".") {
		team += ".cloudflareaccess.com"
	}
	return &AccessVerifier{TeamDomain: "https://" + team, Audience: strings.TrimSpace(audience)}
}

// FromRequest extracts and verifies the Access JWT of r.
func (v *AccessVerifier) FromRequest(r *http.Request) (*Identity, error) {
	tok := r.Header.Get(accessJWTHeader)
	if tok == "" {
		if c, err := r.Cookie(accessCookie); err == nil {
			tok = c.Value
		}
	}
	if tok == "" {
		return nil, errors.New("no Cloudflare Access token: put this hostname behind an Access application")
	}
	return v.Verify(r.Context(), tok)
}

// Verify checks signature (RS256), issuer, audience and expiry.
func (v *AccessVerifier) Verify(ctx context.Context, token string) (*Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed Access token")
	}
	var hdr struct{ Alg, Kid string }
	if err := decodeSegment(parts[0], &hdr); err != nil {
		return nil, fmt.Errorf("Access token header: %w", err)
	}
	if hdr.Alg != "RS256" {
		return nil, fmt.Errorf("unexpected Access token algorithm %q", hdr.Alg)
	}
	key, err := v.key(ctx, hdr.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("malformed Access token signature")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("invalid Access token signature")
	}

	var claims struct {
		Iss        string          `json:"iss"`
		Aud        json.RawMessage `json:"aud"`
		Exp        float64         `json:"exp"`
		Nbf        float64         `json:"nbf"`
		Email      string          `json:"email"`
		CommonName string          `json:"common_name"`
	}
	if err := decodeSegment(parts[1], &claims); err != nil {
		return nil, fmt.Errorf("Access token claims: %w", err)
	}
	if claims.Iss != v.TeamDomain {
		return nil, fmt.Errorf("Access token from %s, expected %s", claims.Iss, v.TeamDomain)
	}
	if !audienceMatches(claims.Aud, v.Audience) {
		return nil, errors.New("Access token is for another application (audience mismatch)")
	}
	now := v.clock()
	const skew = 60 // seconds
	if claims.Exp == 0 || float64(now.Unix()) > claims.Exp+skew {
		return nil, errors.New("Access token expired")
	}
	if claims.Nbf != 0 && float64(now.Unix())+skew < claims.Nbf {
		return nil, errors.New("Access token not valid yet")
	}
	return &Identity{Email: claims.Email, CommonName: claims.CommonName}, nil
}

func (v *AccessVerifier) clock() time.Time {
	if v.now != nil {
		return v.now()
	}
	return time.Now()
}

// key returns the signing key kid, refreshing the key set hourly or when
// an unknown kid appears (Cloudflare rotates keys), at most every 30s.
func (v *AccessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	k, ok := v.keys[kid]
	stale := time.Since(v.fetchedAt) > time.Hour
	if ok && !stale {
		return k, nil
	}
	if !ok && !stale && time.Since(v.fetchedAt) < 30*time.Second {
		return nil, errors.New("Access token signed with an unknown key")
	}
	keys, err := v.fetchKeys(ctx)
	if err != nil {
		if ok {
			return k, nil // keep serving with the cached key
		}
		return nil, fmt.Errorf("fetching Access keys: %w", err)
	}
	v.keys, v.fetchedAt = keys, time.Now()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("Access token signed with an unknown key")
}

// Check fetches the key set once, to validate the configuration.
func (v *AccessVerifier) Check(ctx context.Context) error {
	keys, err := v.fetchKeys(ctx)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.keys, v.fetchedAt = keys, time.Now()
	v.mu.Unlock()
	return nil
}

func (v *AccessVerifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	client := v.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.TeamDomain+"/cdn-cgi/access/certs", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req.URL, resp.Status)
	}
	var set struct {
		Keys []struct {
			Kid, Kty, N, E string
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, err
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(out) == 0 {
		return nil, errors.New("no RSA keys in the Access key set (check the team name)")
	}
	return out, nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// aud is a string or a list of strings.
func audienceMatches(raw json.RawMessage, want string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == want {
				return true
			}
		}
	}
	return false
}
