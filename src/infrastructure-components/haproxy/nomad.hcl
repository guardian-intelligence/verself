variable "guardian_repo_root" {
  type    = string
  default = "/home/ubuntu/.local/state/guardian/repo/current"
}

job "haproxy-upstreams" {
  name = "haproxy-upstreams"
  datacenters = ["*"]
  type = "service"
  group "haproxy-upstreams" {
    count = 1

    network {
      mode = "host"
      port "http" {
        static = 80
        to     = 80
      }
      port "https" {
        static = 443
        to     = 443
      }
    }

    task "setup" {
      driver = "raw_exec"
      user = "root"

      lifecycle {
        hook = "prestart"
        sidecar = false
      }

      config {
        command = "/usr/bin/python3"
        args = ["-c", <<-PY
import os
import json
import pathlib
import pwd
import subprocess
import grp
import hashlib
import shutil
import tarfile
import urllib.parse

def run(args):
    subprocess.run(args, check=True)

def ensure_group(name):
    try:
        import grp
        grp.getgrnam(name)
    except KeyError:
        run(["/usr/sbin/groupadd", "--system", name])

def ensure_user(name):
    try:
        pwd.getpwnam(name)
    except KeyError:
        run([
            "/usr/sbin/useradd",
            "--system",
            "--gid", name,
            "--groups", "adm",
            "--home-dir", "/var/lib/haproxy",
            "--shell", "/usr/sbin/nologin",
            "--no-create-home",
            name,
        ])

ensure_group("haproxy")
ensure_user("haproxy")
haproxy = pwd.getpwnam("haproxy")
adm = grp.getgrnam("adm")

def mkdir(path, uid, gid, mode):
    pathlib.Path(path).mkdir(parents=True, exist_ok=True)
    os.chown(path, uid, gid)
    os.chmod(path, mode)

mkdir("/etc/haproxy", 0, haproxy.pw_gid, 0o750)
mkdir("/etc/haproxy/certs", 0, haproxy.pw_gid, 0o750)
mkdir("/etc/haproxy/maps", 0, haproxy.pw_gid, 0o750)
mkdir("/var/lib/haproxy", haproxy.pw_uid, haproxy.pw_gid, 0o750)
mkdir("/var/lib/haproxy/runtime", 0, haproxy.pw_gid, 0o755)
mkdir("/var/lib/haproxy/runtime/releases", 0, haproxy.pw_gid, 0o755)
mkdir("/var/lib/haproxy/runtime/tmp", 0, haproxy.pw_gid, 0o755)
mkdir("/var/log/haproxy", haproxy.pw_uid, adm.gr_gid, 0o2750)
mkdir("/run/haproxy", haproxy.pw_uid, haproxy.pw_gid, 0o750)

artifact = pathlib.Path("${var.guardian_repo_root}") / "bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar"
if not artifact.is_file():
    raise SystemExit(f"missing HAProxy runtime artifact: {artifact}")

def sha256_file(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

def safe_extract_tar(path, dest):
    dest_resolved = dest.resolve()
    with tarfile.open(path, "r") as tf:
        for member in tf.getmembers():
            target = (dest / member.name).resolve()
            if target != dest_resolved and not str(target).startswith(str(dest_resolved) + os.sep):
                raise SystemExit(f"unsafe runtime artifact member: {member.name}")
            if member.issym() or member.islnk():
                raise SystemExit(f"runtime artifact links are not allowed: {member.name}")
        tf.extractall(dest)

runtime_root = pathlib.Path("/var/lib/haproxy/runtime")
release = runtime_root / "releases" / ("sha256-" + sha256_file(artifact))
tmp = runtime_root / "tmp" / (release.name + "." + str(os.getpid()))
if not (release / "bin/haproxy").is_file() or not (release / "bin/haproxy-upstreams-apply").is_file():
    shutil.rmtree(tmp, ignore_errors=True)
    tmp.mkdir(parents=True, mode=0o755)
    safe_extract_tar(artifact, tmp)
    shutil.rmtree(release, ignore_errors=True)
    os.replace(tmp, release)
else:
    shutil.rmtree(tmp, ignore_errors=True)

next_link = runtime_root / "current.next"
current_link = runtime_root / "current"
try:
    next_link.unlink()
except FileNotFoundError:
    pass
os.symlink(release, next_link)
if current_link.exists() and not current_link.is_symlink():
    shutil.rmtree(current_link)
os.replace(next_link, current_link)

base = pathlib.Path("/etc/haproxy/haproxy.cfg")
doc_path = pathlib.Path("${var.guardian_repo_root}") / "workspace/.guardian/fly/document.json"
try:
    guardian_doc = json.loads(doc_path.read_text(encoding="utf-8"))
except Exception as exc:
    raise SystemExit(f"load Guardian resource graph {doc_path}: {exc}") from exc

resources = {}
for resource in guardian_doc.get("resources", []):
    metadata = resource.get("metadata") or {}
    resources[(resource.get("apiVersion"), resource.get("kind"), metadata.get("name", ""))] = resource

def ref_key(ref):
    return (ref.get("apiVersion"), ref.get("kind"), ref.get("name", ""))

def require_resource(ref):
    key = ref_key(ref)
    resource = resources.get(key)
    if resource is None:
        raise SystemExit(f"missing Guardian resource {key}")
    return resource

def origin_host(origin):
    url = origin.get("spec", {}).get("url", "")
    parsed = urllib.parse.urlparse(url)
    if parsed.scheme != "https" or not parsed.hostname:
        raise SystemExit(f"PublicOrigin {origin.get('metadata', {}).get('name', '')} must declare an https URL")
    return parsed.hostname.lower()

def certificate_matches(host, certificate):
    for dns_name in certificate.get("dnsNames", []):
        dns_name = dns_name.lower()
        if dns_name == host:
            return True
        if dns_name.startswith("*.") and host.endswith(dns_name[1:]) and host != dns_name[2:]:
            return True
    return False

gateway_payload = {"gateways": []}
for gateway in resources.values():
    if gateway.get("apiVersion") != "haproxy.guardianintelligence.org/v1alpha1" or gateway.get("kind") != "HAProxyGateway":
        continue
    gateway_spec = gateway.get("spec", {})
    certificates_for_gateway = [
        {
            "name": certificate.get("name", ""),
            "dnsNames": certificate.get("dnsNames", []),
            "pemPath": certificate.get("pemPath", ""),
        }
        for certificate in gateway_spec.get("certificates", [])
    ]
    projected = {
        "name": gateway.get("metadata", {}).get("name", ""),
        "origins": [],
        "certificates": certificates_for_gateway,
        "routes": [],
    }
    for origin_ref in gateway_spec.get("origins", []):
        origin = require_resource(origin_ref)
        host = origin_host(origin)
        certificate = next((candidate for candidate in certificates_for_gateway if certificate_matches(host, candidate)), {})
        projected["origins"].append({
            "name": origin.get("metadata", {}).get("name", ""),
            "url": origin.get("spec", {}).get("url", ""),
            "host": host,
            "certificate": certificate,
        })
    for route in gateway_spec.get("routes", []):
        hostname = route.get("hostname", "")
        backend = route.get("backend", "")
        if not hostname or not backend:
            raise SystemExit(f"invalid HAProxy route: {route!r}")
        projected["routes"].append({
            "name": route.get("name", ""),
            "hostname": hostname,
            "backend": backend,
            "paths": route.get("paths", []),
            "pathPrefixes": route.get("pathPrefixes", []),
        })
    gateway_payload["gateways"].append(projected)

if not gateway_payload["gateways"]:
    raise SystemExit("Guardian resource graph does not declare an HAProxyGateway")

routes = []
origins = []
certificates = {}
for gateway in gateway_payload.get("gateways", []):
    for origin in gateway.get("origins", []):
        host = origin.get("host", "")
        if host:
            origins.append(host)
        certificate = origin.get("certificate", {})
        pem_path = certificate.get("pemPath", "")
        if pem_path:
            certificates[pem_path] = certificate
    for certificate in gateway.get("certificates", []):
        pem_path = certificate.get("pemPath", "")
        if pem_path:
            certificates[pem_path] = certificate
    for route in gateway.get("routes", []):
        hostname = route.get("hostname", "")
        backend = route.get("backend", "")
        if not hostname or not backend:
            raise SystemExit(f"invalid gateway route: {route!r}")
        routes.append((hostname, backend, route.get("paths", []), route.get("pathPrefixes", [])))

public_hosts = sorted(set(origins + [hostname for hostname, _, _, _ in routes]))
if not public_hosts:
    raise SystemExit("Guardian HAProxyGateway did not declare any public origins or routes")
if not certificates:
    raise SystemExit("Guardian HAProxyGateway did not declare any public certificates")

for pem_path in sorted(certificates):
    pem = pathlib.Path(pem_path)
    if not pem.is_file():
        raise SystemExit(f"missing HAProxy public certificate {pem_path}")
    os.chown(pem, 0, haproxy.pw_gid)
    os.chmod(pem, 0o640)

def haproxy_string(value):
    if any(ch.isspace() for ch in value):
        raise SystemExit(f"unsupported whitespace in HAProxy token: {value!r}")
    return value

ready_hosts = " ".join(haproxy_string(host) for host in public_hosts)
route_lines = []
for index, (hostname, backend, paths, path_prefixes) in enumerate(routes, start=1):
    host_acl = f"route_{index}_host"
    route_lines.append(f"  acl {host_acl} var(txn.host) -m str {haproxy_string(hostname)}")
    if paths or path_prefixes:
        path_acl = f"route_{index}_path"
        if paths:
            route_lines.append(f"  acl {path_acl} path -i {' '.join(haproxy_string(path) for path in paths)}")
        if path_prefixes:
            route_lines.append(f"  acl {path_acl} path_beg {' '.join(haproxy_string(path) for path in path_prefixes)}")
        route_lines.append(f"  use_backend {haproxy_string(backend)} if {host_acl} {path_acl}")
    else:
        route_lines.append(f"  use_backend {haproxy_string(backend)} if {host_acl}")

base.write_text("""global
  maxconn 1024
  stats socket /run/haproxy/admin.sock mode 660 level admin expose-fd listeners
  crt-base /etc/haproxy/certs
  ssl-default-bind-options ssl-min-ver TLSv1.3 no-tls-tickets

defaults
  mode http
  timeout connect 5s
  timeout client 30s
  timeout server 30s

frontend fe_http
  bind 0.0.0.0:80
  http-request redirect scheme https code 308

frontend fe_https
  bind 0.0.0.0:443 ssl crt /etc/haproxy/certs strict-sni alpn h2,http/1.1
  http-request set-var(txn.host) req.hdr(host),lower,field(1,:)
  http-request set-header X-Forwarded-Host %[req.hdr(host)]
  http-request set-header X-Forwarded-Proto https
  acl guardian_ready path -i /.well-known/guardian/ready
  acl guardian_ready_host var(txn.host) -m str """ + ready_hosts + """
  http-request return status 200 content-type text/plain string "guardian haproxy ready" if guardian_ready guardian_ready_host
""" + "\n".join(route_lines) + """
  default_backend be_not_found
backend be_not_found
  http-request return status 404
""", encoding="utf-8")
os.chown(base, 0, haproxy.pw_gid)
os.chmod(base, 0o640)
PY
        ]
      }

      resources {
        cpu = 50
        memory = 64
      }
    }

    task "haproxy-upstreams" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "5s"

      config {
        args = ["--source", "local/nomad-upstreams.cfg", "--dest", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-bin", "/var/lib/haproxy/runtime/current/bin/haproxy", "--haproxy-config", "/etc/haproxy/haproxy.cfg", "--haproxy-config", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-ld-library-path", "/var/lib/haproxy/runtime/current/lib/haproxy", "--reload-unit", "", "--restart-pid-file", "/run/haproxy/haproxy.pid", "--daemon"]
        command = "/var/lib/haproxy/runtime/current/bin/haproxy-upstreams-apply"
      }
      env {
        OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4317"
      }
      resources {
        cpu = 50
        memory = 64
      }
      restart {
        attempts = 3
        delay = "5s"
        interval = "60s"
        mode = "delay"
      }
      template {
        change_mode = "script"
        destination = "local/nomad-upstreams.cfg"
        data = <<-EOT
# Authored Nomad service-catalog template for HAProxy upstream membership.

backend be_firecracker_forgejo
  guid be_firecracker_forgejo
  http-request set-header Host %[req.hdr(host)]
  http-request set-header X-Forwarded-Host %[req.hdr(host)]
[[ with nomadService "forgejo-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_firecracker_forgejo_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_npm_registry_verdaccio
  guid be_route_product_npm_registry_verdaccio
  http-request del-header Authorization
  http-request set-header X-Forwarded-Host %[req.hdr(host)]
  http-request set-header X-Forwarded-Proto https if { ssl_fc }
[[ with nomadService "verdaccio-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_npm_registry_verdaccio_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_auth_zitadel_oidc
  guid be_route_product_auth_zitadel_oidc
  http-request set-header X-Zitadel-Public-Host %[req.hdr(host)]
  http-request set-header X-Zitadel-Instance-Host %[req.hdr(host)]
[[ with nomadService "zitadel-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_auth_zitadel_oidc_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_mail_stalwart_jmap
  guid be_route_product_mail_stalwart_jmap
  acl stalwart_direct path -i /.well-known/jmap
  acl stalwart_direct path_beg /jmap/ /auth/
  acl mail_allowed path -i /.well-known/jmap
  acl mail_allowed path_beg /jmap/ /auth/
  http-request return status 404 unless mail_allowed
[[ with nomadService "stalwart-http" ]]
[[ range $i, $svc := . ]]
  use-server srv_[[ $i ]] if stalwart_direct
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_mail_stalwart_jmap_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_billing_stripe_webhook
  guid be_billing_stripe_webhook
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "billing-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_billing_stripe_webhook_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_firecracker_sandbox_h2c
  guid be_firecracker_sandbox_h2c
  balance random
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_firecracker_sandbox_h2c_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_email_jmap_session
  guid be_email_jmap_session
  balance random
[[ with nomadService "email-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_email_jmap_session_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_company_apex_company_frontend
  guid be_route_company_apex_company_frontend
  balance random
  http-response set-header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
  http-response set-header Cross-Origin-Opener-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy strict-origin-when-cross-origin
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
[[ with nomadService "company-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_company_apex_company_frontend_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_dashboard_grafana_operator_ui
  guid be_route_product_dashboard_grafana_operator_ui
  balance random
  http-response set-header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
  http-response set-header Cross-Origin-Opener-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy strict-origin-when-cross-origin
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request set-header X-Forwarded-Host %[req.hdr(host)]
  http-request set-header X-Forwarded-Proto https if { ssl_fc }
[[ with nomadService "grafana-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_dashboard_grafana_operator_ui_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_apex_iam_service_public_api
  guid be_route_product_apex_iam_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "iam-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_apex_iam_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_apex_verself_web_frontend
  guid be_route_product_apex_verself_web_frontend
  balance random
  http-response set-header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
  http-response set-header Cross-Origin-Opener-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy strict-origin-when-cross-origin
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
[[ with nomadService "verself-web-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_apex_verself_web_frontend_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_analytics_api_analytics_service_otlp_http
  guid be_route_product_analytics_api_analytics_service_otlp_http
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path /v1/logs }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "analytics-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_analytics_api_analytics_service_otlp_http_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_billing_api_billing_public_api
  guid be_route_product_billing_api_billing_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "billing-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_billing_api_billing_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_deployments_api_deployment_service_public_api
  guid be_route_product_deployments_api_deployment_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl deployment_service_allowed path -i /healthz
  acl deployment_service_allowed path_beg /api/v1
  http-request return status 404 unless deployment_service_allowed
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "deployment-service-public-api" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_deployments_api_deployment_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_distribution_api_distribution_service_public_api
  guid be_route_product_distribution_api_distribution_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl distribution_allowed path -i /v2/
  acl distribution_allowed path_beg /api/v1 /v2/
  http-request return status 404 unless distribution_allowed
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "distribution-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_distribution_api_distribution_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_git_source_code_hosting_service_git_smart_http
  guid be_route_product_git_source_code_hosting_service_git_smart_http
  balance random
  acl source_git method GET POST
  acl source_git_path path_reg ^/[^/]+/[^/]+(\.git)?/(info/refs|git-upload-pack|git-receive-pack)$
  http-request return status 404 unless source_git source_git_path
  http-request set-header X-Forwarded-Host %[req.hdr(host)]
  http-request set-header X-Forwarded-Proto https
[[ with nomadService "source-code-hosting-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_git_source_code_hosting_service_git_smart_http_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_github_api_github_integration_service_public_api
  guid be_route_product_github_api_github_integration_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path -i /api/v1/github/webhooks }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "github-integration-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_github_api_github_integration_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_governance_api_governance_service_public_api
  guid be_route_product_governance_api_governance_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "governance-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_governance_api_governance_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_iam_api_iam_service_public_api
  guid be_route_product_iam_api_iam_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "iam-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_iam_api_iam_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_email_api_email_service_public_api
  guid be_route_product_email_api_email_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "email-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_email_api_email_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_notifications_api_notifications_service_public_api
  guid be_route_product_notifications_api_notifications_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 16385 if has_content_length
  http-request wait-for-body time 1s at-least 16385 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 16384 }
[[ with nomadService "notifications-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_notifications_api_notifications_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_profile_api_profile_service_public_api
  guid be_route_product_profile_api_profile_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 16385 if has_content_length
  http-request wait-for-body time 1s at-least 16385 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 16384 }
[[ with nomadService "profile-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_profile_api_profile_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_projects_api_projects_service_public_api
  guid be_route_product_projects_api_projects_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "projects-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_projects_api_projects_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_sandbox_api_sandbox_rental_public_api
  guid be_route_product_sandbox_api_sandbox_rental_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_sandbox_api_sandbox_rental_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_secrets_api_secrets_service_public_api
  guid be_route_product_secrets_api_secrets_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "secrets-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_secrets_api_secrets_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_route_product_source_api_source_code_hosting_service_public_api
  guid be_route_product_source_api_source_code_hosting_service_public_api
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  http-request return status 404 unless { path_beg /api/v1 }
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "source-code-hosting-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_route_product_source_api_source_code_hosting_service_public_api_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_sandbox_execution_schedule_create
  guid be_sandbox_execution_schedule_create
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_sandbox_execution_schedule_create_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_sandbox_execution_submit
  guid be_sandbox_execution_submit
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_sandbox_execution_submit_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_sandbox_small_json_mutation
  guid be_sandbox_small_json_mutation
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 8193 if has_content_length
  http-request wait-for-body time 1s at-least 8193 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 8192 }
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_sandbox_small_json_mutation_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_source_forgejo_webhook
  guid be_source_forgejo_webhook
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 1048577 if has_content_length
  http-request wait-for-body time 1s at-least 1048577 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 1048576 }
[[ with nomadService "source-code-hosting-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] check inter 1s fall 1 rise 1 guid be_source_forgejo_webhook_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_zitadel_product_token_claims
  guid be_zitadel_product_token_claims
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
  acl has_content_length req.hdr(content-length) -m found
  acl has_transfer_encoding req.hdr(transfer-encoding) -m found
  http-request wait-for-body time 1s at-least 65537 if has_content_length
  http-request wait-for-body time 1s at-least 65537 if has_transfer_encoding
  http-request deny deny_status 413 if { req.body_size gt 65536 }
[[ with nomadService "iam-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_zitadel_product_token_claims_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]
EOT
        perms = "0640"
        left_delimiter = "[["
        right_delimiter = "]]"
        wait {
          min = "100ms"
          max = "1s"
        }
        change_script {
          command = "/var/lib/haproxy/runtime/current/bin/haproxy-upstreams-apply"
          args = ["--source", "local/nomad-upstreams.cfg", "--dest", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-bin", "/var/lib/haproxy/runtime/current/bin/haproxy", "--haproxy-config", "/etc/haproxy/haproxy.cfg", "--haproxy-config", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-ld-library-path", "/var/lib/haproxy/runtime/current/lib/haproxy", "--reload-unit", "", "--restart-pid-file", "/run/haproxy/haproxy.pid"]
          timeout = "5s"
          fail_on_error = true
        }
      }
    }

    task "haproxy" {
      driver       = "raw_exec"
      user         = "root"
      kill_signal  = "SIGTERM"
      kill_timeout = "5s"

      config {
        command = "/bin/sh"
        args = [
          "-ec",
          "while ! test -s /etc/haproxy/nomad-upstreams.cfg; do sleep 0.1; done; exec env LD_LIBRARY_PATH=/var/lib/haproxy/runtime/current/lib/haproxy /var/lib/haproxy/runtime/current/bin/haproxy -Ws -f /etc/haproxy/haproxy.cfg -f /etc/haproxy/nomad-upstreams.cfg -p /run/haproxy/haproxy.pid -S /run/haproxy/master.sock",
        ]
      }

      resources {
        cpu    = 100
        memory = 128
      }

      restart {
        attempts = 3
        delay    = "5s"
        interval = "60s"
        mode     = "delay"
      }

    }

    update {
      max_parallel = 1
      health_check = "task_states"
      min_healthy_time = "1s"
      healthy_deadline = "30s"
      progress_deadline = "60s"
      canary = 0
      auto_revert = true
      auto_promote = false
    }
  }
}
