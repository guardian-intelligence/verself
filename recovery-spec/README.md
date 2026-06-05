# GuardianSpecification

GuardianSpecification defines CRD-style resources for systems that recover
toward declared invariants.

A Guardian resource describes a problem space such as DNS resolution, public
TLS, object storage, or provider authority. Provider plugins implement those
resources for concrete systems such as Cloudflare, Route53, Resend, Stripe,
GitHub, or Latitude.

The resource envelope is intentionally familiar:

```yaml
apiVersion: network.guardian.verself.sh/v1alpha1
kind: DNSResolution
metadata:
  name: gamma-public-dns
spec:
  providerRef: cloudflare-main
```

The first implementation slice is `CloudflareRecovery`, which is being split
into problem-space resources plus a Cloudflare provider binding.

Start with:

- [GuardianSpecification](docs/guardian-specification.md)
- [Resource Model](docs/resource-model.md)
- [Engineering Milestones](docs/engineering-milestones.md)
