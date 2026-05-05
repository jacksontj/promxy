package proxystorage

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/metadata"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

type appenderStub struct{}

// Alerting rules append metrics as well, so we want to make sure we don't *spam* the logs
// when we have real metrics
var appenderLock = sync.Mutex{}
var appenderWarningTime time.Time

func (a *appenderStub) Append(_ storage.SeriesRef, _ labels.Labels, _ int64, _ float64) (storage.SeriesRef, error) {
	appenderLock.Lock()
	now := time.Now()
	if now.Sub(appenderWarningTime) > time.Minute {
		logrus.Warning("No remote_write endpoint defined in promxy")
		appenderWarningTime = now
	}
	appenderLock.Unlock()

	return 0, nil
}

func (a *appenderStub) AppendExemplar(_ storage.SeriesRef, _ labels.Labels, _ exemplar.Exemplar) (storage.SeriesRef, error) {
	return 0, fmt.Errorf("not Implemented")
}

// AppendHistogram is a stub: native histograms are not supported in promxy yet.
// TODO: native histogram support — see follow-up.
func (a *appenderStub) AppendHistogram(_ storage.SeriesRef, _ labels.Labels, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, fmt.Errorf("not Implemented")
}

// AppendHistogramCTZeroSample is a stub.
func (a *appenderStub) AppendHistogramCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, fmt.Errorf("not Implemented")
}

// AppendCTZeroSample is a stub.
func (a *appenderStub) AppendCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _, _ int64) (storage.SeriesRef, error) {
	return 0, nil
}

// UpdateMetadata is a stub.
func (a *appenderStub) UpdateMetadata(_ storage.SeriesRef, _ labels.Labels, _ metadata.Metadata) (storage.SeriesRef, error) {
	return 0, nil
}

// SetOptions is a stub.
func (a *appenderStub) SetOptions(_ *storage.AppendOptions) {}

// Commit submits the collected samples and purges the batch.
func (a *appenderStub) Commit() error { return nil }

func (a *appenderStub) Rollback() error { return nil }
