module github.com/jacksontj/promxy

go 1.13

require (
	github.com/Azure/go-autorest/autorest v0.11.19
	github.com/go-kit/kit v0.10.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/snappy v0.0.3
	github.com/jessevdk/go-flags v1.5.0
	github.com/julienschmidt/httprouter v1.3.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.1-0.20210607165600-196536534fbb
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.29.0
	github.com/prometheus/exporter-toolkit v0.6.0
	github.com/prometheus/prometheus v1.8.2-0.20210707132820-dc8f50559534
	github.com/sirupsen/logrus v1.6.0
	github.com/stretchr/testify v1.7.0
	go.uber.org/atomic v1.8.0
	golang.org/x/time v0.0.0-20210611083556-38a9dc6acbc6
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/klog v1.0.0
)

replace github.com/prometheus/prometheus => github.com/jacksontj/prometheus v1.8.1-0.20210707173926-9bb2f449f97c

replace github.com/golang/glog => github.com/kubermatic/glog-gokit v0.0.0-20181129151237-8ab7e4c2d352

replace k8s.io/klog => github.com/simonpasquier/klog-gokit v0.1.0
