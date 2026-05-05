job "haproxy-upstreams" {
  name = "haproxy-upstreams"
  datacenters = ["dc1"]
  type = "service"
  group "haproxy-upstreams" {
    count = 1
    task "haproxy-upstreams" {
      driver = "raw_exec"
      user = "root"
      kill_signal = "SIGTERM"
      kill_timeout = "5s"
      config {
        args = ["--source", "local/nomad-upstreams.cfg", "--dest", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-bin", "/opt/verself/profile/bin/haproxy", "--haproxy-config", "/etc/haproxy/haproxy.cfg", "--haproxy-config", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-ld-library-path", "/opt/aws-lc/lib/x86_64-linux-gnu", "--reload-unit", "haproxy.service", "--daemon"]
        command = "/opt/verself/profile/bin/haproxy-upstreams-apply"
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

backend be_mailbox_jmap_session
  guid be_mailbox_jmap_session
  balance random
[[ with nomadService "mailbox-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_mailbox_jmap_session_srv_[[ $i ]]
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

backend be_route_product_git_source_code_hosting_service_git_smart_http
  guid be_route_product_git_source_code_hosting_service_git_smart_http
  balance random
  acl source_git method GET POST
  acl source_git_path path_reg ^/[^/]+/[^/]+\.git/(info/refs|git-upload-pack|git-receive-pack)$
  http-request return status 404 unless source_git source_git_path
  http-request set-header X-Forwarded-Host git.verself.sh
  http-request set-header X-Forwarded-Proto https
[[ with nomadService "source-code-hosting-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_git_source_code_hosting_service_git_smart_http_srv_[[ $i ]]
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

backend be_route_product_mail_api_mailbox_service_public_api
  guid be_route_product_mail_api_mailbox_service_public_api
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
[[ with nomadService "mailbox-service-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_mail_api_mailbox_service_public_api_srv_[[ $i ]]
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
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_secrets_api_secrets_service_public_api_srv_[[ $i ]]
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
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_route_product_source_api_source_code_hosting_service_public_api_srv_[[ $i ]]
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

backend be_sandbox_forgejo_actions_webhook
  guid be_sandbox_forgejo_actions_webhook
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
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_sandbox_forgejo_actions_webhook_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_sandbox_github_actions_webhook
  guid be_sandbox_github_actions_webhook
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
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_sandbox_github_actions_webhook_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_sandbox_github_installation_callback
  guid be_sandbox_github_installation_callback
  balance random
  http-response set-header Content-Security-Policy "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
  http-response set-header Cross-Origin-Resource-Policy same-origin
  http-response set-header Permissions-Policy "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"
  http-response set-header Referrer-Policy no-referrer
  http-response set-header X-Content-Type-Options nosniff
  http-response set-header X-Frame-Options DENY
[[ with nomadService "sandbox-rental-public-http" ]]
[[ range $i, $svc := . ]]
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_sandbox_github_installation_callback_srv_[[ $i ]]
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
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_source_forgejo_webhook_srv_[[ $i ]]
[[ end ]]
[[ else ]]
  http-request return status 503 content-type text/plain string "service unavailable"
[[ end ]]

backend be_zitadel_action_api_credentials
  guid be_zitadel_action_api_credentials
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
  server srv_[[ $i ]] [[ $svc.Address ]]:[[ $svc.Port ]] proto h2 check inter 1s fall 1 rise 1 guid be_zitadel_action_api_credentials_srv_[[ $i ]]
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
          command = "/opt/verself/profile/bin/haproxy-upstreams-apply"
          args = ["--source", "local/nomad-upstreams.cfg", "--dest", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-bin", "/opt/verself/profile/bin/haproxy", "--haproxy-config", "/etc/haproxy/haproxy.cfg", "--haproxy-config", "/etc/haproxy/nomad-upstreams.cfg", "--haproxy-ld-library-path", "/opt/aws-lc/lib/x86_64-linux-gnu", "--reload-unit", "haproxy.service"]
          timeout = "5s"
          fail_on_error = true
        }
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
