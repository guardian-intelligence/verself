variable "cluster_name" {
  description = "Name for this verself cluster, used in resource naming."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9-]+$", var.cluster_name))
    error_message = "cluster_name must contain only lowercase alphanumeric characters and hyphens."
  }
}

variable "project_id" {
  description = "Latitude.sh project ID under which all resources are provisioned."
  type        = string
}

variable "worker_count" {
  description = "Number of worker nodes to provision."
  type        = number
  default     = 1

  validation {
    condition     = var.worker_count >= 1
    error_message = "worker_count must be at least 1."
  }
}

variable "infra_count" {
  description = "Number of infrastructure nodes (ClickHouse, controller, Forgejo). Use 0 for single-node dev setup, 3 for production."
  type        = number
  default     = 0

  validation {
    condition     = contains([0, 1, 3, 5], var.infra_count)
    error_message = "infra_count must be 0 (dev), 1, 3, or 5."
  }
}

variable "region" {
  description = "Latitude.sh site code (e.g. ASH, LAX, DAL, CHI, NYC)."
  type        = string
  default     = "ASH"
}

variable "plan" {
  description = "Server plan for worker nodes."
  type        = string
  default     = "f4-metal-medium"
}

variable "infra_plan" {
  description = "Server plan for infrastructure nodes. Can be smaller than workers."
  type        = string
  default     = "s2-medium-x86"
}

variable "operating_system" {
  description = "Operating system slug."
  type        = string
  default     = "ubuntu_24_04_x64_lts"
}

variable "billing" {
  description = "Billing cycle for the servers."
  type        = string
  default     = "hourly"

  validation {
    condition     = contains(["hourly", "monthly"], var.billing)
    error_message = "billing must be either 'hourly' or 'monthly'."
  }
}

variable "ssh_public_key_path" {
  description = "Absolute path to the SSH public key file."
  type        = string

  validation {
    condition     = endswith(var.ssh_public_key_path, ".pub")
    error_message = "ssh_public_key_path should point to a .pub file."
  }
}

variable "r2_backups_enabled" {
  description = "Whether to provision Cloudflare R2 platform recovery storage."
  type        = bool
  default     = false
}

variable "cloudflare_account_id" {
  description = "Cloudflare account ID for R2 platform recovery storage."
  type        = string
  default     = ""

  validation {
    condition     = !var.r2_backups_enabled || can(regex("^[0-9a-f]{32}$", var.cloudflare_account_id))
    error_message = "cloudflare_account_id must be a 32-character hex Cloudflare account ID when R2 backups are enabled."
  }
}

variable "r2_backup_bucket_name" {
  description = "Cloudflare R2 bucket name for platform recovery storage."
  type        = string
  default     = ""

  validation {
    condition     = !var.r2_backups_enabled || can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.r2_backup_bucket_name))
    error_message = "r2_backup_bucket_name must be a valid lowercase R2 bucket name when R2 backups are enabled."
  }
}

variable "r2_backup_bucket_location" {
  description = "Cloudflare R2 location hint for platform recovery storage."
  type        = string
  default     = "ENAM"
}

variable "r2_backup_lock_prefix" {
  description = "Prefix protected by the platform recovery R2 bucket lock."
  type        = string
  default     = "recovery/v1/"
}

variable "r2_backup_lock_retention_seconds" {
  description = "Object lock retention in seconds for platform recovery objects."
  type        = number
  default     = 3024000

  validation {
    condition     = var.r2_backup_lock_retention_seconds >= 86400
    error_message = "r2_backup_lock_retention_seconds must be at least one day."
  }
}
