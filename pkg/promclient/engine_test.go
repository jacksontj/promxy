package promclient

import (
	"context"
	"os"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
)

func TestEngineAPI(t *testing.T) {
	// create test
	content, err := os.ReadFile("testdata/metric_relabel.test")
	if err != nil {
		t.Fatal(err)
	}

	test, err := promql.NewTest(t, string(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := test.Run(); err != nil {
		t.Fatal(err)
	}

	api, err := NewEngineAPI(test.QueryEngine(), test.Queryable())
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
