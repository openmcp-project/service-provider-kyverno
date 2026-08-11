# E2E Tests

The end-to-end tests deploy a full OpenControlPlane environment locally (using [kind](https://kind.sigs.k8s.io/)) and verify the complete lifecycle of the Kyverno service provider: ordering Kyverno into a `ControlPlane`, policy enforcement within the `ControlPlane`, and deletion behaviour.

## Understanding the digest-based version

Kyverno's Helm chart and images are mirrored in the [releasechannel](https://github.com/openmcp-project/releasechannel). The service provider currently only supports digest-based versions, so the test fixture in [`onboarding/kyverno.yaml`](/test/e2e/onboarding/kyverno.yaml) references Kyverno by the SHA-256 digest of its Helm chart:

```yaml
apiVersion: kyverno.services.open-control-plane.io/v1alpha1
kind: Kyverno
metadata:
  name: test-mcp
spec:
  version: "sha256:51a5a3e87e6c9dc254a7045f844dd46aadbdef2e78f27def39e9c5c363ea6265" # v3.8.2
```

### Updating to a newer Kyverno version

#### Prerequisites

- [OCM CLI](https://ocm.software/docs/getting-started/install-the-ocm-cli/)

#### Steps

1. Fetch the latest component version from the releasechannel:

   ```shell
   ocm get cv ghcr.io/openmcp-project/components//github.com/openmcp-project/releasechannel/kyverno --latest -o yaml
   ```

2. In the output, find the resource entry with `name: kyverno` and `type: helmChart`, and copy the `localReference` digest from its `access` block:

   ```yaml
   - component:
       resources:
       - access:
           localReference: sha256:51a5a3e87e6c9dc254a7045f844dd46aadbdef2e78f27def39e9c5c363ea6265  # <- copy this
           mediaType: application/vnd.oci.image.manifest.v1+json
           referenceName: kyverno/kyverno:3.8.2
           type: LocalBlob/v1
         name: kyverno
         type: helmChart
         version: v3.8.2
   ```

3. Update `spec.version` in [`onboarding/kyverno.yaml`](/test/e2e/onboarding/kyverno.yaml) with the new digest and update the version comment.


## Testing your own service provider

For a general guide on how to set up and run e2e tests for a service provider, see the [OpenControlPlane Service Provider Testing documentation](https://open-control-plane.io/developers/serviceprovider/testing).
