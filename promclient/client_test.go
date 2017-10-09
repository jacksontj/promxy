package promclient

import (
	"context"
	"fmt"
	"testing"
)

func TestClient(t *testing.T) {
	url := "http://localhost:8080/api/v1/query?query=scrape_duration_seconds%5B1m%5D&time=1507256489.103&_=1507256486365"
	data, err := GetData(context.Background(), url)
	fmt.Println(data, err)
}
