package tests

import (
	"bytes"
	"compress/zlib"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type client struct {
	t                                      *testing.T
	url                                    string
	key                                    *ecdsa.PrivateKey
	device, token, wallet, remote, session string
	beforeSend                             func(*http.Request)
}

func key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func public(t *testing.T, k *ecdsa.PrivateKey) string {
	t.Helper()
	b, e := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(b)
}
func runtimeURL(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("WALLET_TEST_URL")
	if endpoint == "" {
		t.Skip("set WALLET_TEST_URL to test the real upstream runtime")
	}
	return strings.TrimRight(endpoint, "/")
}

func TestRuntime(t *testing.T) {
	endpoint := runtimeURL(t)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Get(endpoint + "/actuator/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("runtime status: %d", response.StatusCode)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "UP" {
		t.Fatalf("unexpected health: %q", health.Status)
	}
	if revision := os.Getenv("UPSTREAM_REVISION"); revision != "" {
		if len(revision) != 40 || response.Header.Get("X-Service-Version") != revision[:7] {
			t.Fatalf("service revision %q does not match %q", response.Header.Get("X-Service-Version"), revision)
		}
	}
}

func (c *client) request(method, path string, body any, headers http.Header, signers map[string]*ecdsa.PrivateKey, want int) map[string]json.RawMessage {
	c.t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r, e := http.NewRequest(method, c.url+path, bytes.NewReader(b))
	if e != nil {
		c.t.Fatal(e)
	}
	if headers != nil {
		r.Header = headers.Clone()
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		d := sha256.Sum256(b)
		r.Header.Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(d[:])+":")
	}
	if len(signers) > 0 {
		fields := []string{"@method", "@path"}
		for _, h := range []string{"Mdvm-Wi-Id", "Wpb-Wi-Id", "Rwsca-Account-Id", "Auth-Challenge", "Mdvm-Token", "Rwsca-Pin-Session-Token", "Content-Digest"} {
			if r.Header.Get(h) != "" {
				fields = append(fields, strings.ToLower(h))
			}
		}
		var inputs, sigs []string
		for label, k := range signers {
			quoted := make([]string, len(fields))
			lines := make([]string, len(fields))
			for i, f := range fields {
				quoted[i] = fmt.Sprintf("%q", f)
				v := r.Header.Get(f)
				if f == "@method" {
					v = method
				}
				if f == "@path" {
					v = path
				}
				lines[i] = fmt.Sprintf("%q: %s", f, v)
			}
			input := "(" + strings.Join(quoted, " ") + ");keyid=\"local,device\";alg=\"ecdsa-p256-sha256\";created=" + fmt.Sprint(time.Now().Unix())
			lines = append(lines, "\"@signature-params\": "+input)
			d := sha256.Sum256([]byte(strings.Join(lines, "\n")))
			sig, e := ecdsa.SignASN1(rand.Reader, k, d[:])
			if e != nil {
				c.t.Fatal(e)
			}
			inputs = append(inputs, label+"="+input)
			sigs = append(sigs, label+"=:"+base64.StdEncoding.EncodeToString(sig)+":")
		}
		r.Header.Set("Signature-Input", strings.Join(inputs, ","))
		r.Header.Set("Signature", strings.Join(sigs, ","))
	}
	if c.beforeSend != nil {
		c.beforeSend(r)
	}
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		c.t.Fatal(e)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, data)
	}
	out := map[string]json.RawMessage{}
	if len(data) > 0 {
		if e := json.Unmarshal(data, &out); e != nil {
			c.t.Fatalf("decode %s: %v", path, e)
		}
	}
	return out
}
func str(t *testing.T, m map[string]json.RawMessage, k string) string {
	t.Helper()
	var s string
	if e := json.Unmarshal(m[k], &s); e != nil {
		t.Fatalf("%s: %v", k, e)
	}
	return s
}
func (c *client) challenge(service string) string {
	return str(c.t, c.request("POST", "/v1/"+service+"/challenge", nil, nil, nil, 200), service+"_auth_challenge")
}
func (c *client) auth(service string) http.Header {
	h := http.Header{}
	h.Set("Auth-Challenge", c.challenge(service))
	h.Set("Mdvm-Token", c.token)
	if service == "wpb" {
		h.Set("Wpb-Wi-Id", c.wallet)
	}
	if service == "rwsca" {
		h.Set("Rwsca-Account-Id", c.remote)
	}
	return h
}
func (c *client) call(service, action string, body any, extra map[string]*ecdsa.PrivateKey, want int) map[string]json.RawMessage {
	signers := map[string]*ecdsa.PrivateKey{service + "-auth-sig": c.key}
	for k, v := range extra {
		signers[k] = v
	}
	return c.request("POST", "/v1/"+service+"/"+action, body, c.auth(service), signers, want)
}
func (c *client) register(platform string) {
	body := map[string]any{"wi_mdvm_auth_pubk": public(c.t, c.key), "wi_device_class": map[string]string{"platform": platform, "systemVersion": "18.0", "model": "local-test"}}
	if platform == "ios" {
		body["pap_devicecheck_attestation"] = ""
		body["pap_devicecheck_assertion"] = ""
	} else {
		body["wi_android_key_attestation"] = []string{}
	}
	h := http.Header{}
	h.Set("Auth-Challenge", c.challenge("mdvm"))
	h.Set("Skip-Integrity-Checks", "true")
	r := c.request("POST", "/v1/mdvm/"+platform+"/register", body, h, map[string]*ecdsa.PrivateKey{"mdvm-auth-sig": c.key}, 200)
	c.device = str(c.t, r, "mdvm_wi_id")
	c.token = str(c.t, r, "mdvm_token")
	if _, e := time.Parse(time.RFC3339Nano, str(c.t, r, "mdvm_token_expiration_time")); e != nil {
		c.t.Fatal(e)
	}
}
func jwt(t *testing.T, token, typ string) map[string]json.RawMessage {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("not a compact JWT")
	}
	decode := func(s string) []byte {
		b, e := base64.RawURLEncoding.DecodeString(s)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	var h struct {
		Alg, Typ string
		X5c      []string
	}
	if e := json.Unmarshal(decode(parts[0]), &h); e != nil {
		t.Fatal(e)
	}
	if h.Alg != "ES256" || h.Typ != typ || len(h.X5c) == 0 {
		t.Fatalf("header: %+v", h)
	}
	der, e := base64.StdEncoding.DecodeString(h.X5c[0])
	if e != nil {
		t.Fatal(e)
	}
	cert, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	response, e := http.Get(runtimeURL(t) + "/mock/ca.pem")
	if e != nil {
		t.Fatal(e)
	}
	rootData, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	rootPEM, _ := pem.Decode(rootData)
	if rootPEM == nil {
		t.Fatal("missing public trust anchor")
	}
	root, e := x509.ParseCertificate(rootPEM.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, e := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); e != nil {
		t.Fatalf("attestation certificate is not trusted by the local CA: %v", e)
	}
	sig := decode(parts[2])
	d := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(cert.PublicKey.(*ecdsa.PublicKey), d[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("invalid JWT signature")
	}
	var claims map[string]json.RawMessage
	if e := json.Unmarshal(decode(parts[1]), &claims); e != nil {
		t.Fatal(e)
	}
	return claims
}

func TestWalletLifecycle(t *testing.T) {
	for _, platform := range []string{"ios", "android"} {
		t.Run(platform, func(t *testing.T) {
			endpoint := runtimeURL(t)
			c := &client{t: t, url: endpoint, key: key(t)}
			c.register(platform)
			assertKeyBinding(t, jwt(t, c.token, "mdvm-token+jwt"), c.key)
			r := c.call("wpb", "register", nil, nil, 200)
			c.wallet = str(t, r, "wpb_wi_id")
			rev := str(t, r, "wpb_wi_revocation_code")
			if !strings.HasPrefix(rev, "rev1") {
				t.Fatal("revocation code is not Bech32")
			}
			c.call("wpb", "register", nil, nil, 409)
			wiaKey := key(t)
			r = c.call("wpb", "attestation", map[string]any{"wi_wia_pubk": public(t, wiaKey)}, map[string]*ecdsa.PrivateKey{"wpb-wia-sig": wiaKey}, 200)
			wia := jwt(t, str(t, r, "wpb_wia"), "oauth-client-attestation+jwt")
			assertKeyBinding(t, wia, wiaKey)
			var issued, expires int64
			json.Unmarshal(wia["iat"], &issued)
			json.Unmarshal(wia["exp"], &expires)
			if expires-issued != 600 {
				t.Fatal("WIA must expire after ten minutes")
			}
			clientID := str(t, r, "wpb_client_instance_id")
			var status struct {
				Status struct {
					List struct {
						URI   string `json:"uri"`
						Index int    `json:"idx"`
					} `json:"status_list"`
				} `json:"status"`
			}
			if e := json.Unmarshal(wia["client_status"], &status); e != nil {
				t.Fatal(e)
			}
			r = c.call("wpb", "attestation", map[string]any{"wi_wia_pubk": public(t, wiaKey), "wpb_client_instance_id": clientID}, map[string]*ecdsa.PrivateKey{"wpb-wia-sig": wiaKey}, 200)
			if str(t, r, "wpb_client_instance_id") != clientID {
				t.Fatal("renewal allocated a different entry")
			}
			c.remote = str(t, c.call("rwsca", "register", nil, nil, 200), "rwsca_account_id")
			c.call("rwsca", "startPinSession", nil, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": key(t)}, 409)
			pin := key(t)
			r = c.call("rwsca", "initializePinAndStartPinSession", map[string]string{"wi_rwsca_pin_pubk": public(t, pin)}, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": pin}, 200)
			c.session = str(t, r, "rwsca_pin_session_token")
			r = c.call("rwsca", "createKeys", map[string]any{"number_of_keys": 2, "pp_c_nonce": "issuer-nonce"}, nil, 200)
			wte := jwt(t, str(t, r, "rwsca_wte"), "key-attestation+jwt")
			if str(t, wte, "nonce") != "issuer-nonce" {
				t.Fatal("WTE nonce mismatch")
			}
			var keys []struct {
				Public  string `json:"rwscd_wi_pubk"`
				Wrapped string `json:"rwsca_wi_wrapped_prvk"`
			}
			if e := json.Unmarshal(r["rwsca_wi_keys"], &keys); e != nil || len(keys) != 2 {
				t.Fatalf("keys: %v", e)
			}
			var attested []json.RawMessage
			if e := json.Unmarshal(wte["attested_keys"], &attested); e != nil || len(attested) != len(keys) {
				t.Fatal("WTE does not attest every returned key")
			}
			for i, entry := range keys {
				der, e := base64.StdEncoding.DecodeString(entry.Public)
				if e != nil {
					t.Fatal(e)
				}
				pk, e := x509.ParsePKIXPublicKey(der)
				if e != nil {
					t.Fatal(e)
				}
				assertJWK(t, attested[i], pk.(*ecdsa.PublicKey))
			}
			digest := sha256.Sum256([]byte("wallet key binding input"))
			headers := c.auth("rwsca")
			headers.Set("Rwsca-Pin-Session-Token", c.session)
			r = c.request("POST", "/v1/rwsca/signData", map[string]string{"rwsca_wi_wrapped_prvk": keys[0].Wrapped, "wi_key_binding_data_hash": base64.StdEncoding.EncodeToString(digest[:])}, headers, map[string]*ecdsa.PrivateKey{"rwsca-auth-sig": c.key}, 200)
			signature, _ := base64.StdEncoding.DecodeString(str(t, r, "rwscd_key_binding_signature"))
			der, _ := base64.StdEncoding.DecodeString(keys[0].Public)
			pk, e := x509.ParsePKIXPublicKey(der)
			if e != nil || !ecdsa.VerifyASN1(pk.(*ecdsa.PublicKey), digest[:], signature) {
				t.Fatal("signature does not verify over original digest")
			}
			c.call("pns", "register", map[string]string{"mpp_registration_token": "local-push-token"}, nil, 204)
			c.request("POST", "/v1/wpb/revoke", map[string]string{"wpb_wi_revocation_code": rev}, nil, nil, 202)
			c.request("POST", "/v1/wpb/revoke", map[string]string{"wpb_wi_revocation_code": rev}, nil, nil, 200)
			c.call("wpb", "attestation", map[string]string{"wi_wia_pubk": public(t, wiaKey)}, map[string]*ecdsa.PrivateKey{"wpb-wia-sig": wiaKey}, 403)
			res, e := http.Get(endpoint + strings.TrimPrefix(status.Status.List.URI, endpoint))
			if e != nil {
				t.Fatal(e)
			}
			defer res.Body.Close()
			data, _ := io.ReadAll(res.Body)
			if res.StatusCode != 200 {
				t.Fatalf("status list: %s", data)
			}
			claims := jwt(t, string(data), "statuslist+jwt")
			var list struct {
				Bits    int    `json:"bits"`
				Encoded string `json:"lst"`
			}
			json.Unmarshal(claims["status_list"], &list)
			compressed, _ := base64.RawURLEncoding.DecodeString(list.Encoded)
			zr, e := zlib.NewReader(bytes.NewReader(compressed))
			if e != nil {
				t.Fatal(e)
			}
			bits, _ := io.ReadAll(zr)
			zr.Close()
			if list.Bits != 1 || bits[status.Status.List.Index/8]&(1<<uint(status.Status.List.Index%8)) == 0 {
				t.Fatal("revocation not reflected in status list")
			}
		})
	}
}

func assertKeyBinding(t *testing.T, claims map[string]json.RawMessage, key *ecdsa.PrivateKey) {
	t.Helper()
	var cnf struct {
		JWK json.RawMessage `json:"jwk"`
	}
	if e := json.Unmarshal(claims["cnf"], &cnf); e != nil {
		t.Fatal(e)
	}
	assertJWK(t, cnf.JWK, &key.PublicKey)
}

func assertJWK(t *testing.T, data json.RawMessage, key *ecdsa.PublicKey) {
	t.Helper()
	var jwk struct{ Kty, Crv, X, Y string }
	if e := json.Unmarshal(data, &jwk); e != nil {
		t.Fatal(e)
	}
	x, xe := base64.RawURLEncoding.DecodeString(jwk.X)
	y, ye := base64.RawURLEncoding.DecodeString(jwk.Y)
	if jwk.Kty != "EC" || jwk.Crv != "P-256" || xe != nil || ye != nil || new(big.Int).SetBytes(x).Cmp(key.X) != 0 || new(big.Int).SetBytes(y).Cmp(key.Y) != 0 {
		t.Fatal("JWT is bound to the wrong public key")
	}
}

func TestPinFailuresAndMalformedRequests(t *testing.T) {
	endpoint := runtimeURL(t)
	c := &client{t: t, url: endpoint, key: key(t)}
	c.register("android")
	c.remote = str(t, c.call("rwsca", "register", nil, nil, 200), "rwsca_account_id")
	pin := key(t)
	c.call("rwsca", "initializePinAndStartPinSession", map[string]string{"wi_rwsca_pin_pubk": public(t, pin)}, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": pin}, 200)
	// Missing proof metadata is not a wrong PIN attempt.
	for i := 0; i < 4; i++ {
		c.call("rwsca", "startPinSession", nil, nil, 401)
	}
	c.call("rwsca", "startPinSession", nil, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": pin}, 200)
	for i := 0; i < 3; i++ {
		want := 401
		if i == 2 {
			want = 403
		}
		c.call("rwsca", "startPinSession", nil, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": key(t)}, want)
	}
	c.call("rwsca", "startPinSession", nil, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": pin}, 403)
	c.call("rwsca", "initializePinAndStartPinSession", map[string]string{"wi_rwsca_pin_pubk": public(t, pin)}, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": pin}, 403)
	c.call("rwsca", "createKeys", map[string]any{"number_of_keys": 0, "pp_c_nonce": "x"}, nil, 400)
	c.request("POST", "/v1/wpb/register", nil, http.Header{}, nil, 400)
	c.request("GET", "/v1/wpb/challenge", nil, nil, nil, 405)
}
