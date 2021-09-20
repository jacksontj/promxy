package proxystorage

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/sirupsen/logrus"
)

type appenderStub struct{}

// Alerting rules append metrics as well, so we want to make sure we don't *spam* the logs
// when we have real metrics
var appenderLock = sync.Mutex{}
var appenderWarningTime time.Time

func (a *appenderStub) Append(ref uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	appenderLock.Lock()
	now := time.Now()
	if now.Sub(appenderWarningTime) > time.Minute {
		logrus.Warning("No remote_write endpoint defined in promxy")
		appenderWarningTime = now
	}
	appenderLock.Unlock()

	return 0, nil
}

func (a *appenderStub) AppendExemplar(ref uint64, l labels.Labels, e exemplar.Exemplar) (uint64, error) {
	return 0, fmt.Errorf("not Implemented")
}

// Commit submits the collected samples and purges the batch.
func (a *appenderStub) Commit() error { return nil }

func (a *appenderStub) Rollback() error { return nil }
