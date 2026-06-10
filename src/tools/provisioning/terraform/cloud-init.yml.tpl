#cloud-config
# Minimal cloud-init: ensure Python is available for Ansible and SSH keys are set.
# All real configuration is handled by Ansible after provisioning.

package_update: true
packages:
  - python3
  - python3-apt

# The provider OS image ships pre-generated host keys (built on a CI runner),
# so every box imaged from it shares one host private key. Delete them and
# generate per-box keys on first boot; operator tooling pins ed25519 only.
ssh_deletekeys: true
ssh_genkeytypes:
  - ed25519

ssh_authorized_keys:
  - ${ssh_public_key}
