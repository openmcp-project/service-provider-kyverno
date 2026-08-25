[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/service-provider-kyverno)](https://api.reuse.software/info/github.com/openmcp-project/service-provider-kyverno)

# 🛡️ service-provider-kyverno

A service provider for managing [Kyverno](https://kyverno.io/) deployments within a OpenControlPlane environment. This provider enables policy enforcement capabilities by automatically installing and configuring Kyverno on managed control planes.

## 📖 Overview

The Kyverno service provider automates the lifecycle management of Kyverno installations, including:

- 🔄 **Automated Kyverno Deployment** - Deploys Kyverno via Helm to `ControlPlanes`
- 🔑 **Secret Management** - Automatic copying of registry credentials across cluster boundaries
- 📊 **Status Tracking** - Status reporting of all managed resources

## 🏗️ Architecture

```mermaid
flowchart LR

  subgraph PC[Platform Cluster]
    spkyverno[Service Provider Kyverno]
    subgraph TN[Tenant Namespace]
      ocirepo([OCIRepository])
      helmrel([HelmRelease])
    end
  end

  subgraph OC[Onboarding Cluster]
    kyvernoapi([Kyverno])
    mcpapi([ControlPlane])

    kyvernoapi -- references --> mcpapi
  end

  subgraph mcp[ControlPlane]
    subgraph KS[kyverno namespace]
      kyvernoctrl[Kyverno Controllers]
      pullsecret([image-pull-secret])
    end
  end

  spkyverno -- reconciles --> kyvernoapi
  spkyverno -- creates --> ocirepo
  spkyverno -- creates --> helmrel
  helmrel -- installs --> kyvernoctrl
  spkyverno -- copies secrets --> pullsecret
  mcpapi -- represents --> mcp
```

## 🚦 Getting Started

### Prerequisites

- Go 1.21+
- [Task](https://taskfile.dev/) (task runner)
- Docker (for building images)
- Access to an OpenControlPlane environment

### 🛠️ Local Development

1. **Clone the repository**

   ```bash
   git clone https://github.com/openmcp-project/service-provider-kyverno.git
   cd service-provider-kyverno
   ```

2. **Install dependencies**

   ```bash
   go mod download
   ```

3. **Build the binary**

   ```bash
   task build
   ```

4. **Run tests**

   ```bash
   task test
   ```

5. **Build the container image**

   ```bash
   task build:img:build
   ```

### 🧪 Running End-to-End Tests

```bash
task test-e2e
```

This will build the image and run the full e2e test suite.

## 📦 Installation

To install the Kyverno service provider, create a `ServiceProvider` resource in your platform cluster:

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: kyverno
  namespace: openmcp-system
spec:
  image: ghcr.io/openmcp-project/images/service-provider-kyverno:v0.1.0
```

## 📝 API Reference

### Kyverno

The `Kyverno` resource represents a Kyverno installation on a `ControlPlane`.

```yaml
apiVersion: kyverno.services.open-control-plane.io/v1alpha1
kind: Kyverno
metadata:
  name: my-kyverno
  namespace: default
spec:
  version: "3.3.7"
```

| Field          | Type   | Description                       |
| -------------- | ------ | --------------------------------- |
| `spec.version` | string | The version of Kyverno to install |

Note that the version must match one of the versions defined in the `ProviderConfig`.

### ProviderConfig

The `ProviderConfig` resource configures deployment settings for the Kyverno service provider.

```yaml
apiVersion: kyverno.services.open-control-plane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: kyverno
spec:
  # Optional: Reconciliation interval
  pollInterval: "1m"
  # Optional: OCI URL of the Kyverno Helm chart
  chartURL: "oci://ghcr.io/kyverno/charts"
  # Optional: Secret for private chart registry
  imagePullSecret:
    name: "image-registry-credentials"
  # Optional: Custom Helm values passed directly to the HelmRelease
  values:
    replicaCount: 3
```

| Field                  | Type     | Description                                                                     |
| ---------------------- | -------- | ------------------------------------------------------------------------------- |
| `spec.pollInterval`    | duration | How often to reconcile resources (default: `1m`)                                |
| `spec.chartURL`        | string   | OCI registry URL for the Kyverno Helm chart (default: `ghcr.io/kyverno/charts`) |
| `spec.imagePullSecret` | object   | Secret name for chart registry authentication                                   |
| `spec.values`          | object   | Custom Helm values passed directly to the managed HelmRelease                   |

## 🔧 Development Tasks

| Command                | Description                |
| ---------------------- | -------------------------- |
| `task build`           | Build the binary           |
| `task build:img:build` | Build the container image  |
| `task test`            | Run unit tests             |
| `task test-e2e`        | Run end-to-end tests       |
| `task generate`        | Generate CRDs and code     |
| `task validate`        | Run linters and formatters |

## Quality Criteria

[![Quality: Incubating](https://img.shields.io/badge/Quality-Incubating-3d9970?style=flat-square&labelColor=555)](https://open-control-plane.io/developers/serviceprovider/quality-criteria)

| Criterion                         | Status | Notes                                                                                                                                                                                             |
| --------------------------------- | :----: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deletion behaviour                |   ✅    |                                                                                                                                                                                                   |
| Status reporting & error messages |   ✅    |                                                                                                                                                                                                   |
| Operation annotations             |   ⚠️    | `openmcp.cloud/operation: ignore` is processed by [opencontrolplane-runtime](https://github.com/openmcp-project/opencontrolplane-runtime). `openmcp.cloud/operation: reconcile` is not processed. |
| API stability policy              |   ✅    |                                                                                                                                                                                                   |
| Custom CA support                 |   ❌    | Custom CA bundle propagation to Kyverno components is not implemented.                                                                                                                            |
| Release artifacts (image + OCM)   |   ✅    |                                                                                                                                                                                                   |
| Testing                           |   ✅    |                                                                                                                                                                                                   |
| Ownership and maintenance docs    |   ✅    |                                                                                                                                                                                                   |

See the [OpenControlPlane Quality Criteria](https://open-control-plane.io/developers/serviceprovider/quality-criteria) for definitions.

## 🤝 Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/service-provider-kyverno/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](https://github.com/openmcp-project/.github/blob/main/CONTRIBUTING.md).

## 🔒 Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/service-provider-kyverno/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## 📜 Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/openmcp-project/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## 📄 Licensing

Copyright OpenControlPlane contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/service-provider-kyverno).

---

<p align="center">
  <a href="https://apeirora.eu/content/projects/">
    <img alt="BMWK-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="300"/>
  </a>
</p>

<p align="center">
  OpenControlPlane is part of <a href="https://apeirora.eu/content/projects/">ApeiroRA</a>, an EU Important Project of Common European Interest (IPCEI-CIS).
</p>

<p align="center">
  Copyright Linux Foundation Europe. For web site terms of use, trademark policy and other project policies please see <a href="https://linuxfoundation.eu/en/policies">https://linuxfoundation.eu/en/policies</a>.
</p>
