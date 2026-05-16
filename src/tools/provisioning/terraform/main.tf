# The latitudesh provider authenticates via the LATITUDESH_AUTH_TOKEN
# environment variable. No credentials are stored in configuration.
provider "latitudesh" {}

# The Cloudflare provider authenticates via the CLOUDFLARE_API_TOKEN
# environment variable. Keep provider credentials out of OpenTofu state.
provider "cloudflare" {}

# -----------------------------------------------------------------
# SSH Key
# -----------------------------------------------------------------
resource "latitudesh_ssh_key" "verself" {
  name       = "verself-${var.cluster_name}"
  public_key = trimspace(file(var.ssh_public_key_path))
}

# -----------------------------------------------------------------
# User Data
# -----------------------------------------------------------------
resource "latitudesh_user_data" "verself" {
  description = "verself-${var.cluster_name} bootstrap"
  content = base64encode(templatefile("${path.module}/cloud-init.yml.tpl", {
    ssh_public_key = trimspace(file(var.ssh_public_key_path))
  }))
}

# -----------------------------------------------------------------
# Worker nodes
# -----------------------------------------------------------------
resource "latitudesh_server" "worker" {
  count            = var.worker_count
  hostname         = "vs-${var.cluster_name}-w${count.index}"
  plan             = var.plan
  site             = var.region
  operating_system = var.operating_system
  project          = var.project_id
  billing          = var.billing
  ssh_keys         = [latitudesh_ssh_key.verself.id]
  allow_reinstall  = true
  user_data        = latitudesh_user_data.verself.id
}

# -----------------------------------------------------------------
# Infrastructure nodes (ClickHouse, controller, Forgejo)
# -----------------------------------------------------------------
resource "latitudesh_server" "infra" {
  count            = var.infra_count
  hostname         = "vs-${var.cluster_name}-i${count.index}"
  plan             = var.infra_plan
  site             = var.region
  operating_system = var.operating_system
  project          = var.project_id
  billing          = var.billing
  ssh_keys         = [latitudesh_ssh_key.verself.id]
  allow_reinstall  = true
  user_data        = latitudesh_user_data.verself.id
}

# -----------------------------------------------------------------
# Platform recovery storage
# -----------------------------------------------------------------
resource "cloudflare_r2_bucket" "platform_recovery" {
  count = var.r2_backups_enabled ? 1 : 0

  account_id    = var.cloudflare_account_id
  name          = var.r2_backup_bucket_name
  location      = upper(var.r2_backup_bucket_location)
  storage_class = "Standard"

  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_r2_bucket_lock" "platform_recovery" {
  count = var.r2_backups_enabled ? 1 : 0

  account_id  = var.cloudflare_account_id
  bucket_name = cloudflare_r2_bucket.platform_recovery[0].name
  rules = [{
    id = "platform-recovery-retention"
    condition = {
      max_age_seconds = var.r2_backup_lock_retention_seconds
      type            = "Age"
    }
    enabled = true
    prefix  = var.r2_backup_lock_prefix
  }]

  lifecycle {
    prevent_destroy = true
  }
}
