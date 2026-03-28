#cloud-config
# Minimal cloud-init: ensure Python is available for Ansible and SSH keys are set.
# All real configuration is handled by Ansible after provisioning.

package_update: true
packages:
  - python3
  - python3-apt

ssh_authorized_keys:
  - ${ssh_public_key}
