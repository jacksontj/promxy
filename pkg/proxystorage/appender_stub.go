package proxystorage

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

type appenderStub struct{}

// Alerting rules append metrics as well, so we want to make sure we don't *spam* the logs
// when we have real metrics
var appenderLock = sync.Mutex{}
var appenderWarningTime time.Time

func (a *appenderStub) Append(ref storage.SeriesRef, l labels.Labels, t int64, v float64) (storage.SeriesRef, error) {
	appenderLock.Lock()
	now := time.Now()
	if now.Sub(appenderWarningTime) > time.Minute {
		logrus.Warning("No remote_write endpoint defined in promxy")
		appenderWarningTime = now
	}
	appenderLock.Unlock()

	return 0, nil
}

func (a *appenderStub) AppendExemplar(ref storage.SeriesRef, l labels.Labels, e exemplar.Exemplar) (storage.SeriesRef, error) {
	return 0, fmt.Errorf("not Implemented")
}

// Commit submits the collected samples and purges the batch.
func (a *appenderStub) Commit() error { return nil }

func (a *appenderStub) Rollback() error { return nil }
