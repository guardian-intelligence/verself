package siteinject

import (
	"testing"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-tools/internal/siteconfig"
)

func TestApplyReplacesTaskEnvAndTemplates(t *testing.T) {
	jobID := "web"
	job := &api.Job{
		ID: &jobID,
		TaskGroups: []*api.TaskGroup{{
			Tasks: []*api.Task{{
				Env: map[string]string{
					"PRODUCT_BASE_URL": "__VERSELF_PRODUCT_BASE_URL__",
				},
				Config: map[string]any{
					"endpoint": "__VERSELF_BILLING_SERVICE_BASE_URL__",
				},
				Templates: []*api.Template{{
					EmbeddedTmpl: stringPtr("issuer=__VERSELF_AUTH_ISSUER_URL__"),
				}},
			}},
		}},
	}
	model := siteconfig.Model{
		Site:              "gamma",
		ProductDomain:     "gamma.verself.sh",
		CompanyDomain:     "gamma.guardianintelligence.org",
		ZitadelDomain:     "gamma.verself.sh",
		SpiffeTrustDomain: "gamma.verself.sh",
		InstallationID:    "inst_gamma",
		Domains: map[string]string{
			"billing_service": "billing.api.gamma.verself.sh",
		},
	}
	if err := Apply(job, model); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := job.TaskGroups[0].Tasks[0].Env["PRODUCT_BASE_URL"]; got != "https://gamma.verself.sh" {
		t.Fatalf("env = %q", got)
	}
	if got := job.TaskGroups[0].Tasks[0].Config["endpoint"]; got != "https://billing.api.gamma.verself.sh" {
		t.Fatalf("config endpoint = %q", got)
	}
	if got := *job.TaskGroups[0].Tasks[0].Templates[0].EmbeddedTmpl; got != "issuer=https://gamma.verself.sh" {
		t.Fatalf("template = %q", got)
	}
}

func stringPtr(value string) *string {
	return &value
}
