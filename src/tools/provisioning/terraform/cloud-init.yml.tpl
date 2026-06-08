#cloud-config
# Minimal cloud-init: provide operator SSH access for recovery.

ssh_authorized_keys:
  - ${ssh_public_key}
