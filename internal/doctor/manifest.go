package doctor

// Manifest is the single source of truth for required dev tools.
// Derived from the flake's dev-tools list. Update when flake.lock changes.
var Manifest = []ToolSpec{
	{"go", "go version", "1.25.8", "pkgs.go_1_25"},
	{"tofu", "tofu version -json", "1.11.5", "pkgs.opentofu"},
	{"ansible", "ansible --version", "2.20.3", "pkgs.ansible"},
	{"sops", "sops --version", "3.12.2", "pkgs.sops"},
	{"age", "age --version", "1.3.1", "pkgs.age"},
	{"buf", "buf --version", "1.66.1", "pkgs.buf"},
	{"shellcheck", "shellcheck --version", "0.11.0", "pkgs.shellcheck"},
	{"jq", "jq --version", "1.8.1", "pkgs.jq"},
}
