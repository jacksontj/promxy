package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestUnauthenticatedServerFunctions(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Fatalf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := CreateAndStart(bindAddr, "text", time.Second*5, nil, router, "")
	if err != nil {
		t.Fatalf("an error occured during creation of server: %s", err.Error())
	}

	client := &http.Client{
		Transport: &http.Transport{},
	}

	resp, err := client.Get(fmt.Sprintf("http://%s/metrics", bindAddr))
	if err != nil {
		t.Fatalf("could not make request to metrics endpoint: %s", err.Error())
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read response body: %s", err.Error())
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("an unexpected error occurred: unauthenticed client was unable to make a request to the unauthenticated server. Response body: %s", body)
	}

	if !strings.Contains(string(body), "go_goroutines") {
		t.Fatalf("could not find metric name 'go_goroutines' in response")
	}
	server.Close()
}

func TestAuthenticatedServerDoesNotStartupWithInvalidConfig(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Fatalf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := CreateAndStart(bindAddr, "text", time.Second*5, nil, router, "testdata/invalid-tls-server-config.yml")
	if err == nil {
		t.Fatalf("server validated an invalid tlsConfig")
	}

	if server != nil {
		server.Close()
	}
}

func TestMutualTLSClientCannotConnectToAuthenticatedServerWithoutCerts(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Fatalf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := CreateAndStart(bindAddr, "text", time.Second*5, nil, router, "testdata/tls-server-config.yml")
	if err != nil {
		t.Fatalf("an error occured during creation of server: %s", err.Error())
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	_, err = client.Get(fmt.Sprintf("https://%s/metrics", bindAddr))
	if err == nil {
		t.Fatalf("was able to make a request to metrics endpoint when it should no have: %s", err.Error())
	}

	server.Close()
}

func TestMutualTLSClientCanConnectToAuthenticatedServerWithCerts(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Fatalf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := CreateAndStart(bindAddr, "text", time.Second*5, nil, router, "testdata/tls-server-config.yml")
	if err != nil {
		t.Fatalf("an error occured during creation of server: %s", err.Error())
	}

	client := setupAuthenticatedClient(t)

	resp, err := client.Get(fmt.Sprintf("https://%s/metrics", bindAddr))
	if err != nil {
		t.Fatalf("could not make request to metrics endpoint: %s", err.Error())
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("could not read response body: %s", err.Error())
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated client was unable to make a request to the authenticated server. Response body: %s", body)
	}

	if !strings.Contains(string(body), "go_goroutines") {
		t.Fatalf("could not find metric name 'go_goroutines' in response")
	}
	server.Close()
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func setupAuthenticatedClient(t *testing.T) *http.Client {
	caCert, err := ioutil.ReadFile("testdata/test-ca.crt")
	if err != nil {
		t.Fatalf("could not read ca certificate: %s", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	clientCert, err := tls.LoadX509KeyPair("testdata/client.crt", "testdata/client.key")
	if err != nil {
		t.Fatalf("could not create keypair from file: %s", err)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates:       []tls.Certificate{clientCert},
				RootCAs:            caCertPool,
				ServerName:         "localhost",
				InsecureSkipVerify: false,
				MinVersion:         tls.VersionTLS12,
				MaxVersion:         tls.VersionTLS13,
			},
		},
	}
}
