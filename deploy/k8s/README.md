# k8s

Promxy doesn't provide any auth mechanisms today. If you require auth in your setup I recommend using either:

1. k8s' ingress -- https://github.com/kubernetes-retired/contrib/tree/master/ingress/controllers/nginx/examples/auth 
2. setting up nginx (or another proxy) as the auth endpoint -- https://prometheus.io/docs/guides/basic-auth/ 
