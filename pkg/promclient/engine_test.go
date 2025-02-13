package promclient

import (
	"context"
	"os"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/prometheus/prometheus/storage"
)

func TestEngineAPI(t *testing.T) {
	// create test
	content, err := os.ReadFile("testdata/metric_relabel.test")
	if err != nil {
		t.Fatal(err)
	}

	test, err := promqltest.NewTest(t, string(content))
	if err != nil {
		t.Fatal(err)
	}

	engineOpts := promql.EngineOpts{
		Logger:                   nil,
		Reg:                      nil,
		MaxSamples:               50000000,
		Timeout:                  10 * time.Minute,
		ActiveQueryTracker:       nil,
		LookbackDelta:            0,
		NoStepSubqueryIntervalFn: func(int64) int64 { return (1 * time.Minute).Milliseconds() },
		EnableAtModifier:         true,
		EnableNegativeOffset:     false,
		EnablePerStepStats:       false,
		EnableDelayedNameRemoval: false,
	}

	queryEngine := promql.NewEngine(engineOpts)

	if err := test.Run(queryEngine); err != nil {
		t.Fatal(err)
	}

	api, err := NewEngineAPI(queryEngine, test.Storage().(storage.SampleAndChunkQueryable))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.TODO()

	t.Run("QueryRange", func(t *testing.T) {
		value, warnings, err := api.QueryRange(ctx, "prometheus_build_info", v1.Range{
			Start: model.Time(0).Time(),
			End:   model.Time(10).Time(),
			Step:  time.Duration(1e6),
		})

		if len(warnings) > 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}

		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}

		matrixValue, ok := value.(model.Matrix)
		if !ok {
			t.Fatalf("unexpected data type: %T", value)
		}
		if len(matrixValue) != 1 {
			t.Fatalf("expecting a single series: %v", matrixValue)
		}
	})

}
