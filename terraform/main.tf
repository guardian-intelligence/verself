# The latitudesh provider authenticates via the LATITUDESH_AUTH_TOKEN
# environment variable. No credentials are stored in configuration.
provider "latitudesh" {}

# -----------------------------------------------------------------
# SSH Key
# -----------------------------------------------------------------
resource "latitudesh_ssh_key" "forge_metal" {
  name       = "forge-metal-${var.cluster_name}"
  public_key = trimspace(file(var.ssh_public_key_path))
}

# -----------------------------------------------------------------
# Worker nodes
# -----------------------------------------------------------------
resource "latitudesh_server" "worker" {
  count            = var.worker_count
  hostname         = "fm-${var.cluster_name}-w${count.index}"
  plan             = var.plan
  site             = var.region
  operating_system = var.operating_system
  project          = var.project_id
  billing          = var.billing
  ssh_keys         = [latitudesh_ssh_key.forge_metal.id]
  allow_reinstall  = true

  user_data = templatefile("${path.module}/cloud-init.yml.tpl", {
    ssh_public_key = trimspace(file(var.ssh_public_key_path))
  })
}

# -----------------------------------------------------------------
# Infrastructure nodes (ClickHouse, controller, Forgejo)
# -----------------------------------------------------------------
resource "latitudesh_server" "infra" {
  count            = var.infra_count
  hostname         = "fm-${var.cluster_name}-i${count.index}"
  plan             = var.infra_plan
  site             = var.region
  operating_system = var.operating_system
  project          = var.project_id
  billing          = var.billing
  ssh_keys         = [latitudesh_ssh_key.forge_metal.id]
  allow_reinstall  = true

  user_data = templatefile("${path.module}/cloud-init.yml.tpl", {
    ssh_public_key = trimspace(file(var.ssh_public_key_path))
  })
}
