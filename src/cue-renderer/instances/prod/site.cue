package topology

// site holds the values that identify a deployment instance: the public
// domains, the human-facing organization labels, and per-site sender
// addresses. Topology shape (components, ports, runtime users) is in
// `config.cue` and `topology.cue`; replacing `site.cue` is the only CUE
// change a second site (e.g. staging) needs against the same graph.
//
// Cross-file unification: this file and `config.cue` both contribute to
// `config.ansible_vars`. CUE merges them at evaluation time. References
// of the form `{{ verself_domain }}` are resolved at Ansible runtime;
// the source file that declared the value does not matter to consumers.
config: ansible_vars: {
	// Public domains. The verself-web app (docs + console + policy) lives at
	// the verself.sh root; there is no separate console subdomain.
	verself_domain: "verself.sh"
	company_domain: "guardianintelligence.org"

	billing_service_subdomain: "billing.api"
	billing_service_domain:    "{{ billing_service_subdomain }}.{{ verself_domain }}"

	sandbox_rental_service_subdomain: "sandbox.api"
	sandbox_rental_service_domain:    "{{ sandbox_rental_service_subdomain }}.{{ verself_domain }}"

	identity_service_subdomain: "identity.api"
	identity_service_domain:    "{{ identity_service_subdomain }}.{{ verself_domain }}"

	profile_service_subdomain: "profile.api"
	profile_service_domain:    "{{ profile_service_subdomain }}.{{ verself_domain }}"

	notifications_service_subdomain: "notifications.api"
	notifications_service_domain:    "{{ notifications_service_subdomain }}.{{ verself_domain }}"

	projects_service_subdomain: "projects.api"
	projects_service_domain:    "{{ projects_service_subdomain }}.{{ verself_domain }}"

	source_code_hosting_service_subdomain: "source.api"
	source_code_hosting_service_domain:    "{{ source_code_hosting_service_subdomain }}.{{ verself_domain }}"

	governance_service_subdomain: "governance.api"
	governance_service_domain:    "{{ governance_service_subdomain }}.{{ verself_domain }}"

	secrets_service_subdomain: "secrets.api"
	secrets_service_domain:    "{{ secrets_service_subdomain }}.{{ verself_domain }}"

	mailbox_service_subdomain: "mail.api"
	mailbox_service_domain:    "{{ mailbox_service_subdomain }}.{{ verself_domain }}"

	forgejo_subdomain: "git"
	forgejo_domain:    "{{ forgejo_subdomain }}.{{ verself_domain }}"

	zitadel_subdomain: "auth"
	zitadel_domain:    "{{ zitadel_subdomain }}.{{ verself_domain }}"

	resend_subdomain:      "notify"
	resend_domain:         "{{ resend_subdomain }}.{{ verself_domain }}"
	resend_sender_address: "noreply@{{ resend_domain }}"
	resend_sender_name:    "verself"

	stalwart_subdomain: "mail"
	stalwart_domain:    "{{ stalwart_subdomain }}.{{ verself_domain }}"

	// Fixed organization identities for this site. The platform org is the
	// dogfooding tenant; the acme org is the canonical fixture customer.
	seed_system_platform_org_name:        "Guardian Intelligence LLC"
	seed_system_platform_org_slug:        "guardian-platform"
	seed_system_acme_org_name:            "Acme Corp"
	seed_system_acme_org_slug:            "acme-corp"
	openbao_tenancy_platform_org_name:    "Guardian Intelligence LLC"
}
