# Provision

Provision owns the bare-metal allocation boundary:

- `terraform/` contains the Latitude.sh OpenTofu project.
- `ansible/` contains the local controller playbooks that apply or destroy
  that OpenTofu state.
- `scripts/generate-inventory.sh` writes the substrate inventory consumed by
  render, substrate, and deploy commands.

Use the explicit command surface:

```bash
aspect provision apply
aspect provision destroy --confirm
```

Provision stops after inventory exists. Host and daemon convergence is
`src/substrate/`; application rollout is Nomad through `aspect deploy`.
