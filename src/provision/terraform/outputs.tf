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
