// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
)

var longErrMessage = strings.Repeat("error message", maxErrMsgLen)

func TestStoreHTTPErrorHandling(t *testing.T) {
	tests := []struct {
		code int
		err  error
	}{
		{
			code: 200,
			err:  nil,
		},
		{
			code: 300,
			err:  fmt.Errorf("server returned HTTP status 300 Multiple Choices: %s", longErrMessage[:maxErrMsgLen]),
		},
		{
			code: 404,
			err:  fmt.Errorf("server returned HTTP status 404 Not Found: %s", longErrMessage[:maxErrMsgLen]),
		},
		{
			code: 500,
			err:  recoverableError{fmt.Errorf("server returned HTTP status 500 Internal Server Error: %s", longErrMessage[:maxErrMsgLen])},
		},
	}

	for i, test := range tests {
		server := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, longErrMessage, test.code)
			}),
		)

		serverURL, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}

		c, err := NewClient(0, &ClientConfig{
			URL:     &config_util.URL{URL: serverURL},
			Timeout: model.Duration(time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}

		err = c.Store(context.Background(), &prompb.WriteRequest{})
		if !reflect.DeepEqual(err, test.err) {
			t.Errorf("%d. Unexpected error; want %v, got %v", i, test.err, err)
		}

		server.Close()
	}
}
