package test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/prometheus/storage"
)

func BenchmarkEvaluations(b *testing.B) {
	//this outer bench isn't real, it only spins through the others, no need to collect data
	b.StopTimer()

	files, err := filepath.Glob("benchdata/*.test")
	if err != nil {
		b.Fatal(err)
	}
	testLoad, err := newTestFromFile(b, "benchdata/load.test")
	if err != nil {
		b.Errorf("error creating test for %s: %s", "benchdata/load.test", err)
	}
	testLoad.Run()

	for _, fn := range files {
		if fn == "benchdata/load.test" {
			continue
		}
		// Create swappable storages
		storageA := &SwappableStorage{}
		storageB := &SwappableStorage{}

		// Create API for the storage engine
		srv, stopChan := startAPIForTest(storageA, ":8083")
		srv2, stopChan2 := startAPIForTest(storageB, ":8084")
		ps := getProxyStorage(rawDoublePSConfig)
		psRemoteRead := getProxyStorage(rawDoublePSConfigRR)

		b.Run(fn, func(b *testing.B) {
			test, err := newTestFromFile(b, fn)
			if err != nil {
				b.Errorf("error creating test for %s: %s", fn, err)
			}
			origStorage := test.Storage()

			b.Run("direct", func(b *testing.B) {
				test.SetStorage(testLoad.Storage())

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					test.Run()
					// We specifically don't check the correctness here, since the values
					// will be off since this isn't aggregating
				}
				b.StopTimer()

				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				srv.Shutdown(ctx)
				<-stopChan
			})

			b.Run("promxy", func(b *testing.B) {
				// set the storage
				storageA.s = testLoad.Storage()
				storageB.s = testLoad.Storage()

				lStorage := &LayeredStorage{ps, testLoad.Storage()}
				// Replace the test storage with the promxy one
				test.SetStorage(lStorage)
				test.QueryEngine().NodeReplacer = ps.NodeReplacer
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					test.Run()
				}

				b.StopTimer()
			})

			b.Run("promxy_remoteread", func(b *testing.B) {
				// set the storage
				storageA.s = testLoad.Storage()
				storageB.s = testLoad.Storage()

				lStorage := &LayeredStorage{psRemoteRead, testLoad.Storage()}
				// Replace the test storage with the promxy one
				test.SetStorage(lStorage)
				test.QueryEngine().NodeReplacer = psRemoteRead.NodeReplacer
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					test.Run()
				}

				b.StopTimer()
			})

			test.SetStorage(origStorage)
			test.Close()
		})

		// stop server
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		srv2.Shutdown(ctx)

		<-stopChan
		<-stopChan2
	}
}

// Swappable storage, to make benchmark perf bearable
type SwappableStorage struct {
	s storage.Storage
}

func (p *SwappableStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return p.s.Querier(ctx, mint, maxt)
}
func (p *SwappableStorage) StartTime() (int64, error) {
	return p.s.StartTime()
}
func (p *SwappableStorage) Appender() (storage.Appender, error) {
	return p.s.Appender()
}
func (p *SwappableStorage) Close() error {
	return p.s.Close()
}
