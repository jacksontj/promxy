package promclient

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/jacksontj/promxy/promhttputil"
)

func DoRequest(ctx context.Context, url string, responseStruct interface{}) error {
	// TODO: cache?
	client := &http.Client{}

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
	defer resp.Body.Close()

	// Read the body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// TODO: check content headers? Prom seems to only do JSON so not necessary
	// for now
	// Unmarshal JSON
	if err := json.Unmarshal(body, responseStruct); err != nil {
		return err
	}
	return nil
}

// HTTP client for prometheus
func GetData(ctx context.Context, url string) (*promhttputil.Response, error) {
	promResp := &promhttputil.Response{}
	if err := DoRequest(ctx, url, promResp); err == nil {
		return promResp, nil
	} else {
		return nil, err
	}
}

func GetSeries(ctx context.Context, url string) (*SeriesResult, error) {
	promResp := &SeriesResult{}
	if err := DoRequest(ctx, url, promResp); err == nil {
		return promResp, nil
	} else {
		return nil, err
	}
}

func GetValuesForLabelName(ctx context.Context, url string) (*LabelResult, error) {
	promResp := &LabelResult{}
	if err := DoRequest(ctx, url, promResp); err == nil {
		return promResp, nil
	} else {
		return nil, err
	}
}
