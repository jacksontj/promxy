module github.com/jacksontj/promxy

go 1.13

require (
	cloud.google.com/go v0.39.0 // indirect
	github.com/Azure/azure-sdk-for-go v30.0.0+incompatible // indirect
	github.com/Azure/go-autorest v11.2.8+incompatible
	github.com/armon/go-metrics v0.0.0-20190430140413-ec5e00d3c878 // indirect
	github.com/aws/aws-sdk-go v1.34.0 // indirect
	github.com/go-kit/kit v0.8.0
	github.com/gogo/protobuf v1.2.1
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/snappy v0.0.1
	github.com/google/gofuzz v1.0.0 // indirect
	github.com/gophercloud/gophercloud v0.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.9.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.1.0 // indirect
	github.com/hashicorp/memberlist v0.1.4 // indirect
	github.com/hashicorp/serf v0.8.3 // indirect
	github.com/jessevdk/go-flags v1.4.0
	github.com/julienschmidt/httprouter v1.2.0
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/miekg/dns v1.1.13 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/opentracing-contrib/go-stdlib v0.0.0-20190519235532-cf7a6c988dc9 // indirect
	github.com/opentracing/opentracing-go v1.1.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.0.1-0.20190709205512-ff1d4e21c12e
	github.com/prometheus/client_model v0.0.0-20190129233127-fd36f4220a90
	github.com/prometheus/common v0.5.0
	github.com/prometheus/prometheus v1.8.1-0.20200513230854-c784807932c2
	github.com/samuel/go-zookeeper v0.0.0-20180130194729-c4fab1ac1bec // indirect
	github.com/shurcooL/httpfs v0.0.0-20190527155220-6a4d4a70508b // indirect
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd // indirect
	github.com/sirupsen/logrus v1.4.3-0.20190518135202-2a22dbedbad1
	github.com/stretchr/testify v1.5.1
	golang.org/x/sys v0.0.0-20190606203320-7fc4e5ec1444 // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4
	google.golang.org/api v0.6.0 // indirect
	google.golang.org/appengine v1.6.1 // indirect
	google.golang.org/genproto v0.0.0-20190605220351-eb0b1bdb6ae6 // indirect
	google.golang.org/grpc v1.21.1 // indirect
	gopkg.in/fsnotify/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/yaml.v2 v2.2.3-0.20190319135612-7b8349ac747c
	k8s.io/klog v0.3.2
	k8s.io/utils v0.0.0-20190529001817-6999998975a7 // indirect
)

replace github.com/prometheus/prometheus => github.com/jacksontj/prometheus v1.8.1-0.20200513230854-c784807932c2

replace github.com/golang/glog => github.com/kubermatic/glog-gokit v0.0.0-20181129151237-8ab7e4c2d352

replace k8s.io/klog => github.com/simonpasquier/klog-gokit v0.1.0
