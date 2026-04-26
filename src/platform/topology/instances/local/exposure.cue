package topology

import (
	"list"
	s "guardianintelligence.org/forge-metal/topology/schema"
)

exposure: s.#Exposure & {
	verself_domain:  "verself.sh"
	platform_domain: "{{ verself_domain }}"
	company_domain:  "guardianintelligence.org"

	origins: {
		console: {
			subdomain: "console"
			domain:    "{{ console_subdomain }}.{{ verself_domain }}"
		}
		billing_service: {
			subdomain: "billing.api"
			domain:    "{{ billing_service_subdomain }}.{{ verself_domain }}"
		}
		sandbox_rental_service: {
			subdomain: "sandbox.api"
			domain:    "{{ sandbox_rental_service_subdomain }}.{{ verself_domain }}"
		}
		identity_service: {
			subdomain: "identity.api"
			domain:    "{{ identity_service_subdomain }}.{{ verself_domain }}"
		}
		profile_service: {
			subdomain: "profile.api"
			domain:    "{{ profile_service_subdomain }}.{{ verself_domain }}"
		}
		notifications_service: {
			subdomain: "notifications.api"
			domain:    "{{ notifications_service_subdomain }}.{{ verself_domain }}"
		}
		projects_service: {
			subdomain: "projects.api"
			domain:    "{{ projects_service_subdomain }}.{{ verself_domain }}"
		}
		source_code_hosting_service: {
			subdomain: "source.api"
			domain:    "{{ source_code_hosting_service_subdomain }}.{{ verself_domain }}"
		}
		governance_service: {
			subdomain: "governance.api"
			domain:    "{{ governance_service_subdomain }}.{{ verself_domain }}"
		}
		secrets_service: {
			subdomain: "secrets.api"
			domain:    "{{ secrets_service_subdomain }}.{{ verself_domain }}"
		}
		mailbox_service: {
			subdomain: "mail.api"
			domain:    "{{ mailbox_service_subdomain }}.{{ verself_domain }}"
		}
		grafana: {
			subdomain: "dashboard"
			domain:    "{{ grafana_subdomain }}.{{ verself_domain }}"
		}
		temporal_web: {
			subdomain: "temporal"
			domain:    "{{ temporal_web_subdomain }}.{{ verself_domain }}"
		}
		forgejo: {
			subdomain: "git"
			domain:    "{{ forgejo_subdomain }}.{{ verself_domain }}"
		}
		zitadel: {
			subdomain: "auth"
			domain:    "{{ zitadel_subdomain }}.{{ verself_domain }}"
		}
	}

	resend_subdomain:      "notify"
	resend_domain:         "{{ resend_subdomain }}.{{ verself_domain }}"
	resend_sender_address: "noreply@{{ resend_domain }}"
	resend_sender_name:    "verself"

	stalwart_subdomain: "mail"
	stalwart_domain:    "{{ stalwart_subdomain }}.{{ verself_domain }}"

	cloudflare_dns_record_type: "A"
	cloudflare_dns_ttl:         1
}

_cloudflareDNSRecords: [
	for _, origin in exposure.origins
	if origin.dns {
		origin.subdomain
	},
	exposure.stalwart_subdomain,
]

_uniqueCloudflareDNSRecords: true & list.UniqueItems(_cloudflareDNSRecords)

ansible: {
	verself_domain:  exposure.verself_domain
	platform_domain: exposure.platform_domain
	company_domain:  exposure.company_domain

	console_subdomain:                     exposure.origins.console.subdomain
	console_domain:                        exposure.origins.console.domain
	billing_service_subdomain:             exposure.origins.billing_service.subdomain
	billing_service_domain:                exposure.origins.billing_service.domain
	sandbox_rental_service_subdomain:      exposure.origins.sandbox_rental_service.subdomain
	sandbox_rental_service_domain:         exposure.origins.sandbox_rental_service.domain
	identity_service_subdomain:            exposure.origins.identity_service.subdomain
	identity_service_domain:               exposure.origins.identity_service.domain
	profile_service_subdomain:             exposure.origins.profile_service.subdomain
	profile_service_domain:                exposure.origins.profile_service.domain
	notifications_service_subdomain:       exposure.origins.notifications_service.subdomain
	notifications_service_domain:          exposure.origins.notifications_service.domain
	projects_service_subdomain:            exposure.origins.projects_service.subdomain
	projects_service_domain:               exposure.origins.projects_service.domain
	source_code_hosting_service_subdomain: exposure.origins.source_code_hosting_service.subdomain
	source_code_hosting_service_domain:    exposure.origins.source_code_hosting_service.domain
	governance_service_subdomain:          exposure.origins.governance_service.subdomain
	governance_service_domain:             exposure.origins.governance_service.domain
	secrets_service_subdomain:             exposure.origins.secrets_service.subdomain
	secrets_service_domain:                exposure.origins.secrets_service.domain
	mailbox_service_subdomain:             exposure.origins.mailbox_service.subdomain
	mailbox_service_domain:                exposure.origins.mailbox_service.domain
	grafana_subdomain:                     exposure.origins.grafana.subdomain
	grafana_domain:                        exposure.origins.grafana.domain
	temporal_web_subdomain:                exposure.origins.temporal_web.subdomain
	temporal_web_domain:                   exposure.origins.temporal_web.domain
	forgejo_subdomain:                     exposure.origins.forgejo.subdomain
	forgejo_domain:                        exposure.origins.forgejo.domain
	zitadel_subdomain:                     exposure.origins.zitadel.subdomain
	zitadel_domain:                        exposure.origins.zitadel.domain

	resend_subdomain:      exposure.resend_subdomain
	resend_domain:         exposure.resend_domain
	resend_sender_address: exposure.resend_sender_address
	resend_sender_name:    exposure.resend_sender_name

	stalwart_subdomain: exposure.stalwart_subdomain
	stalwart_domain:    exposure.stalwart_domain

	cloudflare_dns_records:     _cloudflareDNSRecords
	cloudflare_dns_record_type: exposure.cloudflare_dns_record_type
	cloudflare_dns_ttl:         exposure.cloudflare_dns_ttl
}
