package test

import (
	"context"
	"log"
	"path/filepath"
	"testing"
	"time"

	"net/http"
	_ "net/http/pprof"
	"net/url"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
)

func init() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

func BenchmarkEvaluations(b *testing.B) {
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
				srv, stopChan := startAPIForTest(testLoad.Storage(), ":8085")
				serverURL, _ := url.Parse("http://localhost:8085")
				client, err := remote.NewClient(1, &remote.ClientConfig{
					URL:     &config_util.URL{URL: serverURL},
					Timeout: model.Duration(time.Second),
				})
				if err != nil {
					b.Fatalf("Error creating remote_read client: %v", err)
				}

				lStorage := &RemoteStorage{remote.QueryableClient(client), testLoad.Storage()}
				test.SetStorage(lStorage)

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

type StubStorage struct{}

func (p *StubStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return nil, nil
}
func (p *StubStorage) StartTime() (int64, error) {
	return 0, nil
}
func (p *StubStorage) Appender() (storage.Appender, error) {
	return nil, nil
}
func (p *StubStorage) Close() error {
	return nil
}

type RemoteStorage struct {
	q           storage.Queryable
	baseStorage storage.Storage
}

func (p *RemoteStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return p.q.Querier(ctx, mint, maxt)
}
func (p *RemoteStorage) StartTime() (int64, error) {
	return p.baseStorage.StartTime()
}

func (p *RemoteStorage) Appender() (storage.Appender, error) {
	return p.baseStorage.Appender()
}
func (p *RemoteStorage) Close() error {
	return p.baseStorage.Close()
}
