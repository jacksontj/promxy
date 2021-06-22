package server

import (
	"fmt"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	server := Placeholder(bindAddr, "text", time.Second*5, nil, router)

	client := &http.Client{
		Transport: &http.Transport{
			//TLSClientConfig: &tls.Config{
			//	Certificates:       []tls.Certificate{serverCert},
			//	RootCAs:            caCertPool,
			//	InsecureSkipVerify: true,
			//},
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
