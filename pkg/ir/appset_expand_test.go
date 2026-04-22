package ir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// TestExpandAppSet_List exercises the list-generator path: one Application
// per listElements entry, with template substitution filling {{ .name }} in
// both metadata and destination.
func TestExpandAppSet_List(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name: "appset-list",
		Generators: []types.ArgoAppSetGenerator{
			{
				Kind: types.AppSetGenList,
				ListElements: []map[string]string{
					{"name": "alpha", "cluster": "https://a.example"},
					{"name": "beta", "cluster": "https://b.example"},
				},
			},
		},
		Template: types.ArgoAppSetTemplate{
			Name:    "{{ .name }}",
			Project: "default",
			Destination: types.ArgoDestination{
				Server:    "{{ .cluster }}",
				Namespace: "{{ .name }}",
			},
		},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 2 {
		t.Fatalf("expected 2 Applications, got %d", len(got.Applications))
	}
	if got.Applications[0].Name != "alpha" {
		t.Errorf("Applications[0].Name = %q, want alpha", got.Applications[0].Name)
	}
	if got.Applications[1].Destination.Server != "https://b.example" {
		t.Errorf("Applications[1].Destination.Server = %q, want https://b.example", got.Applications[1].Destination.Server)
	}
	if len(got.Diagnostics) != 0 {
		t.Errorf("expected 0 diagnostics, got %v", got.Diagnostics)
	}
}

// TestExpandAppSet_Matrix exercises the cartesian product of two list
// generators (2x2 = 4 Applications), proving the matrix walker composes
// substitutions across both sides.
func TestExpandAppSet_Matrix(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name: "appset-matrix",
		Generators: []types.ArgoAppSetGenerator{
			{
				Kind: types.AppSetGenMatrix,
				MatrixGenerators: []types.ArgoAppSetGenerator{
					{Kind: types.AppSetGenList, ListElements: []map[string]string{
						{"a": "one"}, {"a": "two"},
					}},
					{Kind: types.AppSetGenList, ListElements: []map[string]string{
						{"b": "red"}, {"b": "blue"},
					}},
				},
			},
		},
		Template: types.ArgoAppSetTemplate{
			Name:        "{{ .a }}-{{ .b }}",
			Destination: types.ArgoDestination{Namespace: "{{ .a }}-{{ .b }}"},
		},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 4 {
		t.Fatalf("expected 4 Applications (2x2), got %d: %+v", len(got.Applications), got.Applications)
	}
	names := map[string]bool{}
	for _, a := range got.Applications {
		names[a.Name] = true
	}
	for _, want := range []string{"one-red", "one-blue", "two-red", "two-blue"} {
		if !names[want] {
			t.Errorf("missing expected Application %q (got %v)", want, names)
		}
	}
}

// TestExpandAppSet_Merge joins two generators on a shared key, verifying
// the secondary's extra fields flow into the result.
func TestExpandAppSet_Merge(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name: "appset-merge",
		Generators: []types.ArgoAppSetGenerator{
			{
				Kind:      types.AppSetGenMerge,
				MergeKeys: []string{"name"},
				MergeGenerators: []types.ArgoAppSetGenerator{
					{Kind: types.AppSetGenList, ListElements: []map[string]string{
						{"name": "a"}, {"name": "b"},
					}},
					{Kind: types.AppSetGenList, ListElements: []map[string]string{
						{"name": "a", "tier": "web"},
						{"name": "b", "tier": "db"},
					}},
				},
			},
		},
		Template: types.ArgoAppSetTemplate{
			Name:        "{{ .name }}-{{ .tier }}",
			Destination: types.ArgoDestination{Namespace: "{{ .name }}"},
		},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 2 {
		t.Fatalf("expected 2 merged Applications, got %d", len(got.Applications))
	}
	wantNames := map[string]bool{"a-web": true, "b-db": true}
	for _, app := range got.Applications {
		if !wantNames[app.Name] {
			t.Errorf("unexpected Application name %q", app.Name)
		}
	}
}

// TestExpandAppSet_PullRequest_WithFixture proves that a fixture injected
// via ExpansionContext.PRFixtures stands in for the live GitHub/GitLab API.
func TestExpandAppSet_PullRequest_WithFixture(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name: "appset-pullrequest",
		Generators: []types.ArgoAppSetGenerator{
			{Kind: types.AppSetGenPullRequest},
		},
		Template: types.ArgoAppSetTemplate{
			Name: "pr-{{ .number }}",
			Source: &types.ArgoSource{
				RepoURL:        "https://gitlab.com/example/repo.git",
				TargetRevision: "{{ .headSha }}",
			},
			Destination: types.ArgoDestination{Namespace: "pr-{{ .number }}"},
		},
	}
	ctx := ExpansionContext{
		PRFixtures: map[string][]map[string]string{
			"appset-pullrequest": {
				{"number": "42", "headSha": "abc"},
				{"number": "43", "headSha": "def"},
			},
		},
	}
	got := ExpandAppSet(as, ctx)
	if len(got.Applications) != 2 {
		t.Fatalf("expected 2 Applications from PR fixture, got %d", len(got.Applications))
	}
	if got.Applications[0].Sources[0].TargetRevision != "abc" {
		t.Errorf("TargetRevision not substituted: %q", got.Applications[0].Sources[0].TargetRevision)
	}
	if got.Applications[1].Name != "pr-43" {
		t.Errorf("expected pr-43, got %q", got.Applications[1].Name)
	}
}

// TestExpandAppSet_PullRequest_NoFixture verifies the info diagnostic path
// when pullRequest has no fixture.
func TestExpandAppSet_PullRequest_NoFixture(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name:       "appset-pullrequest",
		Generators: []types.ArgoAppSetGenerator{{Kind: types.AppSetGenPullRequest}},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 0 {
		t.Fatalf("expected 0 Applications without fixture, got %d", len(got.Applications))
	}
	if len(got.Diagnostics) != 1 {
		t.Fatalf("expected 1 info diagnostic, got %d", len(got.Diagnostics))
	}
	d := got.Diagnostics[0]
	if d.Code != "XPC.H.appset-unsupported-generator" {
		t.Errorf("expected XPC.H.appset-unsupported-generator, got %s", d.Code)
	}
	if d.Severity != types.SeverityInfo {
		t.Errorf("expected info severity, got %s", d.Severity)
	}
}

// TestExpandAppSet_GitDirs walks a real filesystem tree under a temp root.
// Each subdirectory becomes one Application with `path` and `path.basename`
// parameters.
func TestExpandAppSet_GitDirs(t *testing.T) {
	root := t.TempDir()
	appsetFile := filepath.Join(root, "appset.yaml")
	if err := os.WriteFile(appsetFile, []byte("placeholder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"svc-a", "svc-b", "svc-c"} {
		if err := os.MkdirAll(filepath.Join(root, "charts", name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	as := types.ArgoApplicationSet{
		Name:   "git-dirs",
		Source: types.SourceLocation{File: appsetFile},
		Generators: []types.ArgoAppSetGenerator{
			{
				Kind: types.AppSetGenGit,
				Git: &types.ArgoAppSetGitGenerator{
					Directories: []types.ArgoAppSetGitDir{{Path: "charts/*"}},
				},
			},
		},
		Template: types.ArgoAppSetTemplate{
			Name:        "{{ .path.basename }}",
			Destination: types.ArgoDestination{Namespace: "{{ .path.basename }}"},
		},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 3 {
		t.Fatalf("expected 3 Applications from 3 dirs, got %d", len(got.Applications))
	}
}

// TestExpandAppSet_SubstitutesHelmValueFiles proves that {{ .key }}
// placeholders inside source.helm.valueFiles, source.helm.values, and
// source.helm.parameters are resolved through the generator params. This
// mirrors the fg-manifold pattern where ApplicationSet templates reference
// `$values/{{provider}}/{{region}}/{{cluster}}/values.yaml` and helm
// template later fails if the placeholders leak through.
func TestExpandAppSet_SubstitutesHelmValueFiles(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name: "appset-helm-values",
		Generators: []types.ArgoAppSetGenerator{
			{
				Kind: types.AppSetGenList,
				ListElements: []map[string]string{
					{"provider": "aws", "region": "us-east-1", "cluster": "prod", "imageTag": "v1.2.3"},
				},
			},
		},
		Template: types.ArgoAppSetTemplate{
			Name: "{{ .cluster }}",
			Source: &types.ArgoSource{
				RepoURL:        "https://example.com/repo.git",
				Path:           "charts/app",
				TargetRevision: "main",
				Helm: &types.ArgoHelmSource{
					ValueFiles: []string{
						"$values/{{ .provider }}/{{ .region }}/{{ .cluster }}/values.yaml",
						"values-common.yaml",
					},
					Values:      "image:\n  tag: {{ .imageTag }}\n",
					ReleaseName: "{{ .cluster }}-app",
					Parameters: []types.ArgoHelmParam{
						{Name: "image.tag", Value: "{{ .imageTag }}"},
					},
				},
			},
		},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 1 {
		t.Fatalf("expected 1 Application, got %d (diags=%v)", len(got.Applications), got.Diagnostics)
	}
	app := got.Applications[0]
	if len(app.Sources) != 1 || app.Sources[0].Helm == nil {
		t.Fatalf("expected one helm source, got %+v", app.Sources)
	}
	helm := app.Sources[0].Helm
	wantFiles := []string{"$values/aws/us-east-1/prod/values.yaml", "values-common.yaml"}
	if len(helm.ValueFiles) != 2 || helm.ValueFiles[0] != wantFiles[0] || helm.ValueFiles[1] != wantFiles[1] {
		t.Errorf("ValueFiles = %v, want %v", helm.ValueFiles, wantFiles)
	}
	if helm.Values != "image:\n  tag: v1.2.3\n" {
		t.Errorf("Values = %q, want image tag substituted", helm.Values)
	}
	if helm.ReleaseName != "prod-app" {
		t.Errorf("ReleaseName = %q, want prod-app", helm.ReleaseName)
	}
	if len(helm.Parameters) != 1 || helm.Parameters[0].Value != "v1.2.3" {
		t.Errorf("Parameters = %+v, want image.tag=v1.2.3", helm.Parameters)
	}
}

// TestExpandAppSet_HelmSubstitutionDoesNotMutateTemplate guards against a
// subtle bug where reusing the same ApplicationSet template across multiple
// parameter sets (e.g. list generator with N elements) would mutate the
// template's Helm block in place, poisoning later expansions.
func TestExpandAppSet_HelmSubstitutionDoesNotMutateTemplate(t *testing.T) {
	originalFiles := []string{"$values/{{ .cluster }}/values.yaml"}
	tmpl := types.ArgoAppSetTemplate{
		Name: "{{ .cluster }}",
		Source: &types.ArgoSource{
			RepoURL: "https://example.com/repo.git",
			Path:    "charts/app",
			Helm: &types.ArgoHelmSource{
				ValueFiles: originalFiles,
			},
		},
	}
	as := types.ArgoApplicationSet{
		Name: "mutation-guard",
		Generators: []types.ArgoAppSetGenerator{
			{
				Kind: types.AppSetGenList,
				ListElements: []map[string]string{
					{"cluster": "a"},
					{"cluster": "b"},
				},
			},
		},
		Template: tmpl,
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 2 {
		t.Fatalf("expected 2 Applications, got %d", len(got.Applications))
	}
	if got.Applications[0].Sources[0].Helm.ValueFiles[0] != "$values/a/values.yaml" {
		t.Errorf("Applications[0] ValueFiles[0] = %q, want $values/a/values.yaml", got.Applications[0].Sources[0].Helm.ValueFiles[0])
	}
	if got.Applications[1].Sources[0].Helm.ValueFiles[0] != "$values/b/values.yaml" {
		t.Errorf("Applications[1] ValueFiles[0] = %q, want $values/b/values.yaml", got.Applications[1].Sources[0].Helm.ValueFiles[0])
	}
	if tmpl.Source.Helm.ValueFiles[0] != "$values/{{ .cluster }}/values.yaml" {
		t.Errorf("template mutated: ValueFiles[0] = %q", tmpl.Source.Helm.ValueFiles[0])
	}
}

// TestExpandAppSet_TemplateWithRange returns an info diagnostic when the
// template uses unsupported syntax.
func TestExpandAppSet_TemplateWithRange(t *testing.T) {
	as := types.ArgoApplicationSet{
		Name: "ranged",
		Generators: []types.ArgoAppSetGenerator{
			{Kind: types.AppSetGenList, ListElements: []map[string]string{{"name": "a"}}},
		},
		Template: types.ArgoAppSetTemplate{
			Name: "{{range .items}}{{.}}{{end}}",
		},
	}
	got := ExpandAppSet(as, ExpansionContext{})
	if len(got.Applications) != 0 {
		t.Fatalf("expected 0 Applications from unsupported template, got %d", len(got.Applications))
	}
	if len(got.Diagnostics) != 1 {
		t.Fatalf("expected 1 info diagnostic, got %d", len(got.Diagnostics))
	}
	if got.Diagnostics[0].Severity != types.SeverityInfo {
		t.Errorf("expected info severity, got %s", got.Diagnostics[0].Severity)
	}
}
