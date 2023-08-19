test-robust:
	go build -race -mod=vendor -tags netgo,builtinassets && ./promxy --rules.alertbackfill --log-level=debug --http.shutdown-delay=0s --config=demo_robust.conf --web.external-url=http://localhost:8082/promxy


test-local:
	go build -race -mod=vendor -tags netgo,builtinassets && ./promxy --http.shutdown-delay=0s --config=local_test.conf --web.external-url=http://localhost:8082/promxy --log-level=debug

version:
	go build -race -mod=vendor -tags netgo,builtinassets && ./promxy --version
