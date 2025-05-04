# process-exporter

[process-exporter](https://github.com/ncabatoff/process-exporter) is an agent which ships several information about each processes running on the operating system

![Version: 1.0.0](https://img.shields.io/badge/Version-1.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.8.4](https://img.shields.io/badge/AppVersion-0.8.4-informational?style=flat-square)

## Get Repo Info

```console
helm repo add process-exporter https://ncabatoff.github.io/process-exporter
helm repo update
```

## Install Chart

**Important:** only helm3 is supported

```console
helm install [RELEASE_NAME] process-exporter/process-exporter
```

The command deploys process-exporter on the Kubernetes cluster in the default configuration.

_See [configuration](#configuration) below._

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Uninstall Chart

```console
helm uninstall [RELEASE_NAME]
```

This removes all the Kubernetes components associated with the chart and deletes the release.

_See [helm uninstall](https://helm.sh/docs/helm/helm_uninstall/) for command documentation._

## Upgrading Chart

```console
helm upgrade [RELEASE_NAME] [CHART] --install
```

_See [helm upgrade](https://helm.sh/docs/helm/helm_upgrade/) for command documentation._

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| commonLabels | object | `{}` |  |
| configmap.configFile.process_names[0].cmdline[0] | string | `".+"` |  |
| configmap.configFile.process_names[0].name | string | `"{{.Comm}}"` |  |
| configmap.enabled | bool | `true` |  |
| configmap.mountPath | string | `"/config"` |  |
| configmap.name | string | `""` |  |
| containerSecurityContext.readOnlyRootFilesystem | bool | `true` |  |
| daemonsetAnnotations | object | `{}` |  |
| dnsConfig | object | `{}` |  |
| env | object | `{}` |  |
| extraArgs | list | `[]` |  |
| extraVolumeMounts | list | `[]` |  |
| extraVolumes | list | `[]` |  |
| fullnameOverride | string | `""` |  |
| hostIPC | bool | `false` |  |
| hostNetwork | bool | `false` |  |
| hostPID | bool | `true` |  |
| hostProcFsMount.mountPropagation | string | `""` |  |
| image.digest | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ncabatoff/process-exporter"` |  |
| image.tag | string | `""` |  |
| imagePullSecrets | list | `[]` |  |
| livenessProbe.failureThreshold | int | `3` |  |
| livenessProbe.httpGet.httpHeaders | list | `[]` |  |
| livenessProbe.httpGet.path | string | `"/"` |  |
| livenessProbe.httpGet.port | int | `9256` |  |
| livenessProbe.httpGet.scheme | string | `"HTTP"` |  |
| livenessProbe.initialDelaySeconds | int | `0` |  |
| livenessProbe.periodSeconds | int | `10` |  |
| livenessProbe.successThreshold | int | `1` |  |
| livenessProbe.timeoutSeconds | int | `1` |  |
| nameOverride | string | `""` |  |
| namespaceOverride | string | `""` |  |
| nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| podAnnotations."cluster-autoscaler.kubernetes.io/safe-to-evict" | string | `"true"` |  |
| podLabels | object | `{}` |  |
| port.name | string | `"http"` |  |
| port.number | int | `9256` |  |
| readinessProbe.failureThreshold | int | `3` |  |
| readinessProbe.httpGet.httpHeaders | list | `[]` |  |
| readinessProbe.httpGet.path | string | `"/"` |  |
| readinessProbe.httpGet.port | int | `9256` |  |
| readinessProbe.httpGet.scheme | string | `"HTTP"` |  |
| readinessProbe.initialDelaySeconds | int | `0` |  |
| readinessProbe.periodSeconds | int | `10` |  |
| readinessProbe.successThreshold | int | `1` |  |
| readinessProbe.timeoutSeconds | int | `1` |  |
| resources | object | `{}` |  |
| restartPolicy | string | `nil` |  |
| revisionHistoryLimit | int | `10` |  |
| securityContext.fsGroup | int | `65534` |  |
| securityContext.runAsGroup | int | `65534` |  |
| securityContext.runAsNonRoot | bool | `true` |  |
| securityContext.runAsUser | int | `65534` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.automountServiceAccountToken | bool | `false` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.imagePullSecrets | list | `[]` |  |
| serviceAccount.name | string | `nil` |  |
| terminationGracePeriodSeconds | string | `nil` |  |
| tolerations[0].effect | string | `"NoSchedule"` |  |
| tolerations[0].operator | string | `"Exists"` |  |
| updateStrategy.rollingUpdate.maxUnavailable | int | `1` |  |
| updateStrategy.type | string | `"RollingUpdate"` |  |
