package siteinject

import (
	"testing"

	"github.com/hashicorp/nomad/api"

	"github.com/verself/deployment-service/siteconfig"
)

func TestApplyReplacesNestedTokens(t *testing.T) {
	jobID := "web"
	source := "__VERSELF_PRODUCT_BASE_URL__/artifact"
	job := &api.Job{
		ID: &jobID,
		TaskGroups: []*api.TaskGroup{{
			Tasks: []*api.Task{{
				Env: map[string]string{
					"BASE_URL": "__VERSELF_PRODUCT_BASE_URL__",
				},
				Artifacts: []*api.TaskArtifact{{
					GetterSource: &source,
				}},
			}},
		}},
	}
	err := Apply(job, siteconfig.Model{
		Site:              "gamma",
		ProductDomain:     "gamma.verself.sh",
		CompanyDomain:     "guardianintelligence.org",
		ZitadelDomain:     "gamma.verself.sh",
		SpiffeTrustDomain: "spiffe.gamma.verself.sh",
		InstallationID:    "inst_test",
		Domains:           map[string]string{},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := job.TaskGroups[0].Tasks[0].Env["BASE_URL"]; got != "https://gamma.verself.sh" {
		t.Fatalf("BASE_URL = %q", got)
	}
	if got := *job.TaskGroups[0].Tasks[0].Artifacts[0].GetterSource; got != "https://gamma.verself.sh/artifact" {
		t.Fatalf("GetterSource = %q", got)
	}
}
