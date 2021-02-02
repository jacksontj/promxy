module github.com/jacksontj/promxy

go 1.13

require (
	github.com/Azure/go-autorest/autorest v0.11.15
	github.com/go-kit/kit v0.10.0
	github.com/gogo/protobuf v1.3.1
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/snappy v0.0.2
	github.com/jessevdk/go-flags v1.4.0
	github.com/julienschmidt/httprouter v1.3.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.9.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.15.0
	github.com/prometheus/prometheus v1.8.1-0.20200513230854-c784807932c2
	github.com/sirupsen/logrus v1.6.0
	go.uber.org/atomic v1.7.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/klog v1.0.0
)

replace github.com/prometheus/prometheus => github.com/jacksontj/prometheus v1.8.1-0.20210202015034-0e65a22d5597

replace github.com/golang/glog => github.com/kubermatic/glog-gokit v0.0.0-20181129151237-8ab7e4c2d352

replace k8s.io/klog => github.com/simonpasquier/klog-gokit v0.1.0
