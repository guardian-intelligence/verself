# Provisioning Tools

Provisioning tools own the bare-metal allocation boundary:

- `terraform/` contains the Latitude.sh OpenTofu project.
- `ansible/` contains the local controller playbooks that apply or destroy
  that OpenTofu state and write the host inventory consumed by render,
  substrate, and deploy commands.

Use the explicit command surface:

```bash
aspect provision apply
aspect provision destroy --confirm
```

Provisioning stops after inventory exists. Host and daemon convergence is
`src/host-configuration/`; application rollout is Nomad through `aspect deploy`.
