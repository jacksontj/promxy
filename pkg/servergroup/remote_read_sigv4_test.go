package servergroup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	signer "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// TestRemoteReadSigV4 checks that remote_read requests are signed with the
// server group's http_client.sigv4 config, and that the signature covers the
// headers promxy adds (forwarded, http_headers, http_client.http_headers) and
// the request body.
func TestRemoteReadSigV4(t *testing.T) {
	oldInterval := DiscoveryUpdateInterval
	DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { DiscoveryUpdateInterval = oldInterval }()

	const (
		accessKey = "AKIDEXAMPLE"
		secretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
		region    = "us-west-2"
	)
	got := remoteReadRequest(t, `
http_headers:
  X-Scope-OrgID: sg-tenant
http_client:
  http_headers:
    X-Other:
      values: [cc-other]
  sigv4:
    region: `+region+`
    access_key: `+accessKey+`
    secret_key: `+secretKey+`
`, map[string]string{"X-Forwarded-Example": "forwarded"})

	auth := got.header.Get("Authorization")
	wantPrefix := "AWS4-HMAC-SHA256 Credential=" + accessKey + "/"
	if !strings.HasPrefix(auth, wantPrefix) {
		t.Fatalf("remote_read Authorization = %q, want prefix %q", auth, wantPrefix)
	}
	amzDate := got.header.Get("X-Amz-Date")
	if amzDate == "" {
		t.Fatal("remote_read request has no X-Amz-Date header")
	}

	signed := signedHeaders(t, auth)
	for _, h := range []string{"x-scope-orgid", "x-other", "x-forwarded-example", "content-type"} {
		if _, ok := signed[h]; !ok {
			t.Errorf("SignedHeaders %v does not include %q", signed, h)
		}
	}

	// Re-sign the received request with the same credentials and timestamp;
	// a match means the downstream would accept the signature, body included.
	signingTime, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		t.Fatalf("parsing X-Amz-Date %q: %v", amzDate, err)
	}
	req, err := http.NewRequest(got.method, "http://"+got.host+got.path, bytes.NewReader(got.body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for h := range signed {
		if h != "host" && h != "content-length" {
			req.Header[http.CanonicalHeaderKey(h)] = got.header.Values(h)
		}
	}
	sum := sha256.Sum256(got.body)
	creds := aws.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey}
	if err := signer.NewSigner().SignHTTP(context.Background(), creds, req, hex.EncodeToString(sum[:]), "aps", region, signingTime); err != nil {
		t.Fatalf("re-signing request: %v", err)
	}
	if want := req.Header.Get("Authorization"); auth != want {
		t.Errorf("remote_read signature does not verify:\n got  %s\n want %s", auth, want)
	}
}

// signedHeaders returns the SignedHeaders of a SigV4 Authorization header.
func signedHeaders(t *testing.T, auth string) map[string]struct{} {
	t.Helper()
	for _, part := range strings.Split(auth, ", ") {
		if v, ok := strings.CutPrefix(part, "SignedHeaders="); ok {
			out := map[string]struct{}{}
			for _, h := range strings.Split(v, ";") {
				out[h] = struct{}{}
			}
			return out
		}
	}
	t.Fatalf("no SignedHeaders in Authorization %q", auth)
	return nil
}
