# GuardianSpecification

GuardianSpecification defines a lean CRD-style envelope for systems that
recover toward declared invariants.

The core specification only standardizes resource identity. Resource authors
define HomeostaticResourceDefinitions for problem spaces such as DNS
resolution, public TLS, object storage, or provider authority. Provider plugins
implement those resource kinds for concrete systems such as Cloudflare,
Route53, Resend, Stripe, GitHub, or Latitude.

The resource envelope is intentionally familiar:

```yaml
apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec:
  providerRef: cloudflare-main
```

`DNSResolution` is intentionally an example resource, not a built-in envelope
type. Any project may publish its own DNS resolution Homeostatic Resource and
provider plugins may implement it if they conform to its schema.

The current implementation slice is `CloudflareRecovery`, which is being split
into provider-neutral resources plus Cloudflare provider bindings.

Start with:

- [GuardianSpecification](docs/guardian-specification.md)
- [Resource Model](docs/resource-model.md)
- [Engineering Milestones](docs/engineering-milestones.md)
