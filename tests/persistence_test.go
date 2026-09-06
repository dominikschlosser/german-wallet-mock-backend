package tests

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initializeRemote(t *testing.T, c *client) (*ecdsa.PrivateKey, map[string]string) {
	t.Helper()
	c.remote = str(t, c.call("rwsca", "register", nil, nil, 200), "rwsca_account_id")
	pin := key(t)
	r := c.call("rwsca", "initializePinAndStartPinSession", map[string]string{"wi_rwsca_pin_pubk": public(t, pin)}, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": pin}, 200)
	c.session = str(t, r, "rwsca_pin_session_token")
	r = c.call("rwsca", "createKeys", map[string]any{"number_of_keys": 1, "pp_c_nonce": "nonce"}, nil, 200)
	var keys []struct {
		Wrapped string `json:"rwsca_wi_wrapped_prvk"`
	}
	if err := json.Unmarshal(r["rwsca_wi_keys"], &keys); err != nil || len(keys) != 1 {
		t.Fatal("missing wrapped key", err)
	}
	digest := sha256.Sum256([]byte("persistent remote signing"))
	return pin, map[string]string{"rwsca_wi_wrapped_prvk": keys[0].Wrapped, "wi_key_binding_data_hash": base64.StdEncoding.EncodeToString(digest[:])}
}

func signRemote(c *client, body map[string]string, want int) {
	headers := c.auth("rwsca")
	headers.Set("Rwsca-Pin-Session-Token", c.session)
	c.request("POST", "/v1/rwsca/signData", body, headers, map[string]*ecdsa.PrivateKey{"rwsca-auth-sig": c.key}, want)
}

func TestAccountIsolationAndTampering(t *testing.T) {
	endpoint := runtimeURL(t)
	c := &client{t: t, url: endpoint, key: key(t)}
	c.register("ios")
	c.wallet = str(t, c.call("wpb", "register", nil, nil, 200), "wpb_wi_id")
	_, body := initializeRemote(t, c)
	other := &client{t: t, url: endpoint, key: key(t)}
	other.register("android")
	other.wallet = c.wallet
	other.request("DELETE", "/v1/wpb/deleteAccount", nil, other.auth("wpb"), map[string]*ecdsa.PrivateKey{"wpb-auth-sig": other.key}, 404)
	initializeRemote(t, other)
	signRemote(other, body, 401)
	other.session = c.session
	signRemote(other, body, 401)
	c.beforeSend = func(r *http.Request) {
		if r.URL.Path != "/v1/pns/register" {
			return
		}
		data := `{"mpp_registration_token":"tampered"}`
		r.Body = io.NopCloser(strings.NewReader(data))
		r.ContentLength = int64(len(data))
	}
	c.call("pns", "register", map[string]string{"mpp_registration_token": "original"}, nil, 401)
}

func TestRenewalAndServiceDeletion(t *testing.T) {
	for _, platform := range []string{"ios", "android"} {
		t.Run(platform, func(t *testing.T) {
			c := &client{t: t, url: runtimeURL(t), key: key(t)}
			c.register(platform)
			headers := http.Header{}
			headers.Set("Auth-Challenge", c.challenge("mdvm"))
			headers.Set("Skip-Integrity-Checks", "true")
			headers.Set("Mdvm-Wi-Id", c.device)
			body := map[string]any{"wi_device_class": map[string]string{"platform": platform, "systemVersion": "18.0", "model": "local-test"}}
			if platform == "ios" {
				body["pap_devicecheck_assertion"] = ""
			} else {
				body["wi_android_key_attestation"] = []string{}
			}
			r := c.request("POST", "/v1/mdvm/"+platform+"/renewal", body, headers, map[string]*ecdsa.PrivateKey{"mdvm-auth-sig": c.key}, 200)
			c.token = str(t, r, "mdvm_token")
			assertKeyBinding(t, jwt(t, c.token, "mdvm-token+jwt"), c.key)
			c.wallet = str(t, c.call("wpb", "register", nil, nil, 200), "wpb_wi_id")
			c.remote = str(t, c.call("rwsca", "register", nil, nil, 200), "rwsca_account_id")
			c.call("pns", "register", map[string]string{"mpp_registration_token": "delete-test"}, nil, 204)
			for _, service := range []string{"wpb", "rwsca", "pns"} {
				action := "deleteAccount"
				if service == "pns" {
					action = "delete"
				}
				c.request("DELETE", "/v1/"+service+"/"+action, nil, c.auth(service), map[string]*ecdsa.PrivateKey{service + "-auth-sig": c.key}, 204)
			}
			headers.Set("Auth-Challenge", c.challenge("mdvm"))
			c.request("DELETE", "/v1/mdvm/deleteAccount", nil, headers, map[string]*ecdsa.PrivateKey{"mdvm-auth-sig": c.key}, 204)
			c.request("POST", "/v1/mdvm/"+platform+"/renewal", body, headers, map[string]*ecdsa.PrivateKey{"mdvm-auth-sig": c.key}, 404)
		})
	}
}

func TestRestartPreservesAccountsKeysAndPINFailures(t *testing.T) {
	endpoint := runtimeURL(t)
	project := os.Getenv("WALLET_TEST_PROJECT")
	if project == "" {
		t.Skip("set WALLET_TEST_PROJECT to restart the dedicated test stack")
	}
	if !strings.HasPrefix(project, "wallet-test-") {
		t.Fatal("restart tests require a dedicated wallet-test-* Compose project")
	}
	c := &client{t: t, url: endpoint, key: key(t)}
	c.register("ios")
	c.wallet = str(t, c.call("wpb", "register", nil, nil, 200), "wpb_wi_id")
	_, body := initializeRemote(t, c)
	for i := 0; i < 2; i++ {
		c.call("rwsca", "startPinSession", nil, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": key(t)}, 401)
	}
	command := exec.Command("docker", "compose", "-p", project, "restart", "wallet-backend")
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	command.Dir = filepath.Dir(dir)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("restart: %v: %s", err, out)
	}
	ready := false
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get(endpoint + "/actuator/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !ready {
		t.Fatal("upstream server did not recover after restart")
	}
	c.call("wpb", "register", nil, nil, 409)
	signRemote(c, body, 200)
	c.call("rwsca", "startPinSession", nil, map[string]*ecdsa.PrivateKey{"rwsca-pin-sig": key(t)}, 403)
}
