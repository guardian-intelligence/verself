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
  default     = "s3-large-x86"
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
