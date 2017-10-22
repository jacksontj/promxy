package promclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jacksontj/promxy/promhttputil"
)

func DoRequest(ctx context.Context, url string, client *http.Client, responseStruct interface{}) error {
	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	// Pass the context on
	req.WithContext(ctx)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// TODO: check content headers? Prom seems to only do JSON so not necessary
	// for now
	if err := json.NewDecoder(resp.Body).Decode(responseStruct); err != nil {
		return err
	}

	return nil
}

// HTTP client for prometheus
func GetData(ctx context.Context, url string, client *http.Client) (*promhttputil.Response, error) {
	promResp := &promhttputil.Response{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		return promResp, nil
	} else {
		return nil, err
	}
}

func GetSeries(ctx context.Context, url string, client *http.Client) (*SeriesResult, error) {
	promResp := &SeriesResult{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		return promResp, nil
	} else {
		return nil, err
	}
}

func GetValuesForLabelName(ctx context.Context, url string, client *http.Client) (*LabelResult, error) {
	promResp := &LabelResult{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		return promResp, nil
	} else {
		return nil, err
	}
}
