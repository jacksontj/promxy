package server

import (
	"fmt"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServerStartsUp(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Errorf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := Placeholder(bindAddr, "text", time.Second*5, nil, router, "")
	if err != nil {
		t.Errorf("an error occured during creation of server: %s", err.Error())
	}

	client := &http.Client{
		Transport: &http.Transport{},
	}

	resp, err := client.Get(fmt.Sprintf("http://%s/metrics", bindAddr))
	if err != nil {
		t.Errorf("could not make request to metrics endpoint: %s", err.Error())
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("could not read response body: %s", err.Error())
	}

	if !strings.Contains(string(body), "go_goroutines") {
		t.Errorf("could not find metric name 'go_goroutines' in response")
	}
	server.Close()
}

func TestMutualTLSServerCannotConnectWithoutCerts(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Errorf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := Placeholder(bindAddr, "text", time.Second*5, nil, router, "testdata/invalid-tls-server-config.yml")
	if err == nil {
		t.Errorf("server validated an invalid tlsConfig")
	}

	if server != nil {
		server.Close()
	}
}

func TestMutualTLSServerCanConnectWithCerts(t *testing.T) {
	freePort, err := getFreePort()
	if err != nil {
		t.Errorf("could not get a free port to run test: %s", err.Error())
	}
	bindAddr := fmt.Sprintf("localhost:%d", freePort)
	router := httprouter.New()
	router.HandlerFunc("GET", "/metrics", promhttp.Handler().ServeHTTP)

	server, err := Placeholder(bindAddr, "text", time.Second*5, nil, router, "testdata/tls-server-config.yml")
	if err != nil {
		t.Errorf("an error occured during creation of server: %s", err.Error())
	}


	tlsStruct := &web.TLSStruct{
		TLSCertPath: "testdata/server.crt",
		TLSKeyPath:  "testdata/server.key",
		ClientAuth:  "RequireAndVerifyClientCert",
		ClientCAs:   "testdata/test-ca.crt",
	}
	tlsConfig, err := web.ConfigToTLSConfig(tlsStruct)
	if err != nil {
		t.Errorf("an unexpected error occurred translating the tlsStruct to a tlsConfig: %s", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	resp, err := client.Get(fmt.Sprintf("http://%s/metrics", bindAddr))
	if err != nil {
		t.Errorf("could not make request to metrics endpoint: %s", err.Error())
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("could not read response body: %s", err.Error())
	}

	if !strings.Contains(string(body), "go_goroutines") {
		t.Errorf("could not find metric name 'go_goroutines' in response")
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
