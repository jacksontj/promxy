package promclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/mailru/easyjson"
	"github.com/prometheus/common/model"
	"github.com/sirupsen/logrus"
)

func DoRequest(ctx context.Context, url string, client *http.Client, responseStruct easyjson.Unmarshaler) error {
	logrus.Debugf("sending request downstream: %s", url)
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
func GetData(ctx context.Context, url string, client *http.Client, labelset model.LabelSet) (model.Value, error) {
	promResp := &DataResult{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		if promResp.Status != promhttputil.StatusSuccess {
			return nil, fmt.Errorf(promResp.Error)
		}
		if err := promhttputil.ValueAddLabelSet(promResp.Data.Result, labelset); err != nil {
			return nil, err
		}

		return promResp.Data.Result, nil
	} else {
		return nil, err
	}
}

func GetSeries(ctx context.Context, url string, client *http.Client, labelset model.LabelSet) (model.Value, error) {
	promResp := &SeriesResult{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		if promResp.Status != promhttputil.StatusSuccess {
			return nil, fmt.Errorf(promResp.Error)
		}

		// convert to vector (there aren't points, but this way we don't have to make more merging functions)
		retVector := make(model.Vector, len(promResp.Data))
		for j, labelset := range promResp.Data {
			retVector[j] = &model.Sample{
				Metric: model.Metric(labelset),
			}
		}

		if err := promhttputil.ValueAddLabelSet(retVector, labelset); err != nil {
			return nil, err
		}

		return retVector, nil
	} else {
		return nil, err
	}
}

func GetValuesForLabelName(ctx context.Context, url string, client *http.Client) ([]model.LabelValue, error) {
	promResp := &LabelResult{}
	if err := DoRequest(ctx, url, client, promResp); err == nil {
		if promResp.Status != promhttputil.StatusSuccess {
			return nil, fmt.Errorf(promResp.Error)
		}

		return promResp.Data, nil
	} else {
		return nil, err
	}
}
