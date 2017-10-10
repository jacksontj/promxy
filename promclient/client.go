package promclient

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/jacksontj/promxy/promhttputil"
)

// HTTP client for prometheus
func GetData(ctx context.Context, url string) (*promhttputil.Response, error) {
	// TODO: cache?
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Pass the context on
	req.WithContext(ctx)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// TODO: check content headers? Prom seems to only do JSON so not necessary
	// for now
	// Unmarshal JSON
	promResp := &promhttputil.Response{}
	if err := json.Unmarshal(body, promResp); err != nil {
		return nil, err
	}
	return promResp, nil
}

func GetSeries(ctx context.Context, url string) (*SeriesResult, error) {
	// TODO: cache?
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Pass the context on
	req.WithContext(ctx)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read the body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// TODO: check content headers? Prom seems to only do JSON so not necessary
	// for now
	// Unmarshal JSON
	promResp := &SeriesResult{}
	if err := json.Unmarshal(body, promResp); err != nil {
		return nil, err
	}
	return promResp, nil
}
