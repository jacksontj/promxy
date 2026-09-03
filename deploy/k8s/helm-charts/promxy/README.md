# promxy

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.0.93](https://img.shields.io/badge/AppVersion-v0.0.93-informational?style=flat-square)

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/promxy)](https://artifacthub.io/packages/search?repo=promxy)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/jacksontj/promxy/blob/master/LICENSE)

A Helm chart for Promxy, a Prometheus proxy that combines multiple Prometheis

**Homepage:** <https://github.com/jacksontj/promxy>

## Prerequisites

- Kubernetes Kubernetes: `>= 1.25.0-0`
- Helm 3.8+ (for OCI registry support)

## Installation

### Via OCI registry (recommended)

```bash
helm install promxy oci://ghcr.io/jacksontj/helm-charts/promxy \
  --version 0.1.0 \
  --namespace monitoring --create-namespace
```

### Via classic Helm repository

```bash
helm repo add promxy https://jacksontj.github.io/promxy
helm repo update
helm install promxy promxy/promxy \
  --version 0.1.0 \
  --namespace monitoring --create-namespace
```

## Uninstalling

```bash
helm uninstall promxy --namespace monitoring
```

## Configuration

See the table below for all configurable parameters, or inspect [`values.yaml`](./values.yaml).

## Source Code

* <https://github.com/jacksontj/promxy>

## Requirements

Kubernetes: `>= 1.25.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for promxy pod scheduling. |
| annotations | object | `{}` | Annotations to add to the Deployment object. |
| command | list | `[]` | Override the container command. Defaults to the image entrypoint when empty. |
| config | object | `{"global":{"evaluation_interval":"5s","external_labels":{"source":"promxy"}},"promxy":{"server_groups":[{"kubernetes_sd_configs":[{"role":"pod"}]}]},"remote_write":[{"url":"http://localhost:8083/receive"}]}` | Promxy configuration. Rendered into the generated ConfigMap when `configMap` is empty. ref: https://github.com/jacksontj/promxy |
| configMap | string | `""` | Use an existing ConfigMap (or Secret) by name instead of generating one from `.config`. When set, `.config` is ignored. |
| configStorageType | string | `"ConfigMap"` | Storage type for the generated promxy configuration. One of `ConfigMap` or `Secret`. |
| configmapReloader | object | `{"enabled":true,"image":{"pullPolicy":"IfNotPresent","repository":"jimmidyson/configmap-reload","tag":"v0.15.0"},"resources":{"limits":{"cpu":"20m","memory":"20Mi"},"requests":{"cpu":"20m","memory":"20Mi"}},"securityContext":{}}` | Sidecar that watches the ConfigMap and reloads promxy when it changes. |
| configmapReloader.enabled | bool | `true` | Whether to deploy the configmap-reload sidecar. |
| configmapReloader.image.pullPolicy | string | `"IfNotPresent"` | configmap-reload image pull policy. |
| configmapReloader.image.repository | string | `"jimmidyson/configmap-reload"` | configmap-reload image repository. |
| configmapReloader.image.tag | string | `"v0.15.0"` | configmap-reload image tag. |
| configmapReloader.resources | object | `{"limits":{"cpu":"20m","memory":"20Mi"},"requests":{"cpu":"20m","memory":"20Mi"}}` | Resource requests and limits for the configmap-reload sidecar. |
| configmapReloader.securityContext | object | `{}` | Container-level security context for the configmap-reload sidecar. |
| containerPort | int | `8082` | Container port the promxy process listens on. |
| deployment | object | `{"enabled":true,"revisionHistoryLimit":10,"strategy":{}}` | Deployment configuration. ref: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/ |
| deployment.enabled | bool | `true` | Whether to deploy the Deployment object. |
| deployment.revisionHistoryLimit | int | `10` | Number of old ReplicaSets to retain for rollback. ref: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#clean-up-policy |
| deployment.strategy | object | `{}` | Deployment update strategy. ref: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy |
| env | list | `[]` | Environment variables to add to the promxy container. |
| extraArgs | object | `{"log-level":"info"}` | Extra command-line arguments passed to promxy as `--<key>=<value>`. |
| extraEnvFrom | list | `[]` | Sources to populate environment variables in the container, e.g. configMapRef or secretRef. |
| extraFlags | list | `[]` | Extra bare command-line flags passed to promxy as `--<flag>` (no value). |
| extraLabels | object | `{}` | Extra labels to add to every resource created by the chart. |
| extraVolumeMounts | list | `[]` | Extra volume mounts to add to the promxy container. |
| extraVolumes | list | `[]` | Extra volumes to add to the pod (in addition to the config volume). |
| fullnameOverride | string | `""` | Override the fully qualified app name. |
| hpa | object | `{"behavior":{},"enabled":false,"maxReplicas":10,"metrics":[{"resource":{"name":"cpu","target":{"averageUtilization":50,"type":"Utilization"}},"type":"Resource"}],"minReplicas":1}` | HorizontalPodAutoscaler configuration. |
| hpa.behavior | object | `{}` | HPA scaling behavior. ref: https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#configurable-scaling-behavior |
| hpa.enabled | bool | `false` | Whether to create a HorizontalPodAutoscaler for promxy. |
| hpa.maxReplicas | int | `10` | Maximum number of replicas the HPA can scale up to. |
| hpa.metrics | list | `[{"resource":{"name":"cpu","target":{"averageUtilization":50,"type":"Utilization"}},"type":"Resource"}]` | HPA metrics. ref: https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/ |
| hpa.minReplicas | int | `1` | Minimum number of replicas the HPA can scale down to. |
| image | object | `{"pullPolicy":"IfNotPresent","repository":"quay.io/jacksontj/promxy","tag":""}` | Promxy container image configuration. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.repository | string | `"quay.io/jacksontj/promxy"` | Image repository. |
| image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| imagePullSecrets | list | `[]` | List of image pull secrets used to pull container images. |
| ingress | object | `{"annotations":{},"enabled":false,"extraLabels":{},"hosts":["example.com"],"ingressClassName":"nginx","path":"/","pathType":"Prefix","tls":[]}` | Ingress configuration. |
| ingress.annotations | object | `{}` | Annotations to add to the Ingress. |
| ingress.enabled | bool | `false` | Whether to create an Ingress. |
| ingress.extraLabels | object | `{}` | Extra labels to add to the Ingress. |
| ingress.hosts | list | `["example.com"]` | Hostnames the Ingress will accept traffic for. |
| ingress.ingressClassName | string | `"nginx"` | IngressClass that will be used by the Ingress. |
| ingress.path | string | `"/"` | HTTP path for the Ingress rule. |
| ingress.pathType | string | `"Prefix"` | Ingress path type. |
| ingress.tls | list | `[]` | TLS configuration for the Ingress. |
| livenessProbe | object | `{"failureThreshold":6,"httpGet":{"path":"/-/healthy","port":"http","scheme":"HTTP"},"periodSeconds":5,"successThreshold":1,"timeoutSeconds":3}` | Liveness probe for the promxy container. |
| nameOverride | string | `""` | Override the chart name used in resource names and selectors. |
| nodeSelector | object | `{}` | Node selector for promxy pod scheduling. |
| podAnnotations | object | `{}` | Annotations to add to every promxy pod. |
| podDisruptionBudget | object | `{"enabled":false,"labels":{}}` | PodDisruptionBudget configuration. |
| podDisruptionBudget.enabled | bool | `false` | Whether to create a PodDisruptionBudget for promxy pods. |
| podDisruptionBudget.labels | object | `{}` | Extra labels to add to the PodDisruptionBudget. |
| podSecurityContext | object | `{}` | Pod-level security context applied to the promxy pod. |
| priorityClassName | string | `""` | Priority class name for promxy pods. |
| readinessProbe | object | `{"failureThreshold":120,"httpGet":{"path":"/-/ready","port":"http","scheme":"HTTP"},"periodSeconds":5,"successThreshold":1,"timeoutSeconds":3}` | Readiness probe for the promxy container. |
| replicaCount | int | `1` | Number of promxy pod replicas to run. |
| resources | object | `{"limits":{"cpu":"100m","memory":"128Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}` | Resource requests and limits for the promxy container. |
| route | object | `{"enabled":false,"routes":{}}` | Gateway API HTTPRoute configuration. ref: https://gateway-api.sigs.k8s.io/api-types/httproute/ Each key under `route.routes` produces a separate HTTPRoute resource. |
| route.enabled | bool | `false` | Whether to create HTTPRoute resources. |
| route.routes | object | `{}` | Map of HTTPRoute resources to create. Each key is the HTTPRoute name suffix. |
| securityContext | object | `{}` | Container-level security context applied to the promxy container. |
| service | object | `{"annotations":{},"clusterIP":"","enabled":true,"externalIPs":[],"extraLabels":{},"loadBalancerIP":"","loadBalancerSourceRanges":[],"nodePort":"","servicePort":8082,"type":"ClusterIP"}` | Service configuration. |
| service.annotations | object | `{}` | Annotations to add to the Service. |
| service.clusterIP | string | `""` | Static cluster IP for the Service. |
| service.enabled | bool | `true` | Whether to create a Service. |
| service.externalIPs | list | `[]` | External IPs for the Service. |
| service.extraLabels | object | `{}` | Extra labels to add to the Service. |
| service.loadBalancerIP | string | `""` | Static load-balancer IP (when `service.type` is `LoadBalancer`). |
| service.loadBalancerSourceRanges | list | `[]` | CIDRs allowed to reach the Service when `service.type` is `LoadBalancer`. |
| service.nodePort | string | `""` | NodePort to expose when `service.type` is `NodePort`. |
| service.servicePort | int | `8082` | Service port exposed by the Service object. |
| service.type | string | `"ClusterIP"` | Service type. |
| serviceAccount | object | `{"annotations":{},"create":true,"name":null}` | ServiceAccount configuration. |
| serviceAccount.annotations | object | `{}` | Annotations to add to the ServiceAccount. |
| serviceAccount.create | bool | `true` | Whether a ServiceAccount should be created. |
| serviceAccount.name | string | `nil` | Name of the ServiceAccount to use. If not set and `create` is true, a name is generated using the fullname template. |
| serviceMonitor | object | `{"enabled":false,"interval":"30s","labels":{},"metricRelabelings":[],"namespace":"","path":"/metrics","relabelings":[],"scheme":"http","scrapeTimeout":"10s","tlsConfig":{}}` | Prometheus Operator ServiceMonitor configuration. |
| serviceMonitor.enabled | bool | `false` | Whether to create a ServiceMonitor (requires the Prometheus Operator CRDs). |
| serviceMonitor.interval | string | `"30s"` | Scrape interval. |
| serviceMonitor.labels | object | `{}` | Extra labels to add to the ServiceMonitor (often used by Prometheus Operator selectors). |
| serviceMonitor.metricRelabelings | list | `[]` | Relabel rules applied to scraped metrics. |
| serviceMonitor.namespace | string | `""` | Override namespace where the ServiceMonitor is created. |
| serviceMonitor.path | string | `"/metrics"` | HTTP path scraped for metrics. |
| serviceMonitor.relabelings | list | `[]` | Relabel rules applied to scraped targets. |
| serviceMonitor.scheme | string | `"http"` | Scheme used for scraping. |
| serviceMonitor.scrapeTimeout | string | `"10s"` | Scrape timeout. |
| serviceMonitor.tlsConfig | object | `{}` | TLS config for scraping. |
| tolerations | list | `[]` | Tolerations for promxy pod scheduling. |
| topologySpreadConstraints | list | `[]` | Topology spread constraints for promxy pods. |
| verticalAutoscaler | object | `{"enabled":false}` | VerticalPodAutoscaler configuration. |
| verticalAutoscaler.enabled | bool | `false` | Whether to create a VerticalPodAutoscaler for promxy. |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs](https://github.com/norwoodj/helm-docs)
