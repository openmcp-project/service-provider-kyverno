# E2E Tests

The end-to-end tests deploy a full OpenControlPlane environment locally (using [kind](https://kind.sigs.k8s.io/)) and verify the complete lifecycle of the Kyverno service provider: ordering Kyverno into a `ControlPlane`, policy enforcement within the `ControlPlane`, and deletion behaviour.

## Version model

Kyverno's Helm chart and images are mirrored in the [releasechannel](https://github.com/openmcp-project/releasechannel). The `ProviderConfig` declares which versions are available and maps each user-facing version string to the exact chart digest and image coordinates:

```yaml
# platform/providerconfig.yaml
spec:
  versions:
    - version: "v3.8.2"                    # user-facing version (set in Kyverno.spec.version)
      chartVersion: "sha256:..."            # digest of the Helm chart in the releasechannel OCI registry
      chartURL: "oci://ghcr.io/openmcp-project/components/..."
      values:
        admissionController:
          container:
            image:
              registry: ghcr.io
              repository: openmcp-project/components/kyverno/kyverno
              tag: "sha256:..."             # digest of the Kyverno image in the releasechannel OCI registry
```

The `Kyverno` resource in the onboarding cluster simply references the version by its human-readable name:

```yaml
# onboarding/kyverno.yaml
spec:
  version: "v3.8.2"
```

### Updating to a newer Kyverno version

#### Prerequisites

- [OCM CLI](https://ocm.software/docs/getting-started/install-the-ocm-cli/)

#### Steps

1. Fetch the latest component version from the releasechannel:

   ```shell
   ocm get cv ghcr.io/openmcp-project/components//github.com/openmcp-project/releasechannel/kyverno --latest -o yaml
   ```

2. In the output, find the resource with `name: kyverno` and `type: helmChart` — copy its `localReference` digest and `version`:

   ```yaml
   - access:
       localReference: sha256:51a5a3e87e6c9dc254a7045f844dd46aadbdef2e78f27def39e9c5c363ea6265  # -> chartVersion
     name: kyverno
     type: helmChart
     version: v3.8.2                                                                          # -> version
   ```

3. Find the resource with `name: image-kyverno` and `type: ociImage` — copy its `localReference` digest and `version`:

   ```yaml
   - access:
       localReference: sha256:0a540e2ddf74d0d2d3d45f9ef248d7dbc96576accdbcc6a2dd7eaff9fea56504  # -> values.admissionController.container.image.tag
     name: image-kyverno
     type: ociImage
     version: v1.18.2
   ```

4. Add or update the corresponding entry in [`platform/providerconfig.yaml`](/test/e2e/platform/providerconfig.yaml).

5. Update `spec.version` in [`onboarding/kyverno.yaml`](/test/e2e/onboarding/kyverno.yaml) to the new version string.


## Testing your own service provider

For a general guide on how to set up and run e2e tests for a service provider, see the [OpenControlPlane Service Provider Testing documentation](https://open-control-plane.io/developers/serviceprovider/testing).
