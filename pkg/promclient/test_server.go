package promclient

// CreateTestServer simply will create a test HTTP server on the path defined and return
// an API client, a clode method, and any error when creating

//TODO Fix it later
//func CreateTestServer(t *testing.T, path string) (API, func(), error) {
//	var close func()
//	content, err := os.ReadFile(path)
//	if err != nil {
//		return nil, close, err
//	}
//
//	test, err := promqltest.NewTestEngine(t, string(content))
//	if err != nil {
//		return nil, nil, err
//	}
//	close = test.Close
//
//	// Load the data
//	if err := test.Run(); err != nil {
//		return nil, close, err
//	}
//
//	ln, err := net.Listen("tcp", "")
//	if err != nil {
//		return nil, close, err
//	}
//
//	// Start up API server for engine
//	cfgFunc := func() config.Config { return config.DefaultConfig }
//	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
//	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }
//
//	apiRouter := route.New()
//	webv1.NewAPI(
//		test.QueryEngine(), // Query Engine
//		test.Storage().(storage.SampleAndChunkQueryable), // SampleAndChunkQueryable
//		nil, //appendable
//		nil, // exemplarQueryable
//		nil, //factoryTr
//		nil, //factoryAr
//		nil,
//		cfgFunc,
//		nil, // flags
//		webv1.GlobalURLOptions{
//			ListenAddress: ln.Addr().String(),
//			Host:          "localhost",
//			Scheme:        "http",
//		}, // global URL options
//		readyFunc, // ready
//		nil,       // local storage
//		"",        // tsdb dir
//		false,     // enable admin API
//		nil,       // logger
//		nil,       // FactoryRr
//		50000000,  // RemoteReadSampleLimit
//		1000,      // RemoteReadConcurrencyLimit
//		1048576,   // RemoteReadBytesInFrame
//		false,     // isAgent
//		nil,       // CORSOrigin
//		nil,       // runtimeInfo
//		nil,       // buildInfo
//		nil,
//		nil,
//		nil, // gatherer
//		nil, // registerer
//		nil, // statsRenderer
//		false,
//		nil,
//		false,
//	).Register(apiRouter.WithPrefix("/api/v1"))
//
//	srv := &http.Server{Handler: apiRouter}
//	go srv.Serve(ln) // TODO: cancel/stop ability
//	close = func() {
//		//test.Close()
//		srv.Close()
//	}
//
//	client, err := api.NewClient(api.Config{Address: "http://" + ln.Addr().String()})
//	if err != nil {
//		return nil, close, err
//	}
//
//	return &PromAPIV1{clientv1.NewAPI(client)}, close, nil
//}
