output "worker_ips" {
  description = "Public IP addresses of the worker nodes."
  value       = [for s in latitudesh_server.worker : s.primary_ipv4]
}

output "worker_ids" {
  description = "Latitude.sh server IDs of the worker nodes."
  value       = [for s in latitudesh_server.worker : s.id]
}

output "infra_ips" {
  description = "Public IP addresses of the infrastructure nodes."
  value       = [for s in latitudesh_server.infra : s.primary_ipv4]
}

output "infra_ids" {
  description = "Latitude.sh server IDs of the infrastructure nodes."
  value       = [for s in latitudesh_server.infra : s.id]
}

output "r2_backup_bucket_name" {
  description = "Cloudflare R2 platform recovery bucket name."
  value       = var.r2_backups_enabled ? cloudflare_r2_bucket.platform_recovery[0].name : null
}

output "r2_backup_bucket_location" {
  description = "Cloudflare R2 platform recovery bucket location."
  value       = var.r2_backups_enabled ? cloudflare_r2_bucket.platform_recovery[0].location : null
}

output "r2_backup_lock_prefix" {
  description = "Cloudflare R2 platform recovery bucket lock prefix."
  value       = var.r2_backups_enabled ? var.r2_backup_lock_prefix : null
}

output "r2_backup_lock_retention_seconds" {
  description = "Cloudflare R2 platform recovery bucket lock retention in seconds."
  value       = var.r2_backups_enabled ? var.r2_backup_lock_retention_seconds : null
}
