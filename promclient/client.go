package promclient

import (
	"context"
	"net/http"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/mailru/easyjson"
	"github.com/prometheus/common/model"
)

func DoRequest(ctx context.Context, url string, client *http.Client, responseStruct easyjson.Unmarshaler) error {
	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	// Pass the context on
	req = req.WithContext(ctx)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	return easyjson.UnmarshalFromReader(resp.Body, responseStruct)
}

// HTTP client for prometheus
func GetData(ctx context.Context, url string, client *http.Client, labelset model.LabelSet) (*DataResult, error) {
	promResp := &DataResult{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		// TODO: have the client support serverGroup direct
		if err := promhttputil.ValueAddLabelSet(promResp.Data.Result, labelset); err != nil {
			return nil, err
		}

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
