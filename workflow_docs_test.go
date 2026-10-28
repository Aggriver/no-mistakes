package main

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type docsWorkflow struct {
	On          map[string]any `yaml:"on"`
	Concurrency struct {
		Group string `yaml:"group"`
	} `yaml:"concurrency"`
	Jobs map[string]struct {
		If    string   `yaml:"if"`
		Steps []wfStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func loadDocsWorkflow(t *testing.T) docsWorkflow {
	t.Helper()
	raw, err := os.ReadFile(".github/workflows/docs.yml")
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	var workflow docsWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	return workflow
}

func TestDocsWorkflowUsesScopedConcurrencyGroup(t *testing.T) {
	workflow := loadDocsWorkflow(t)
	if got, want := workflow.Concurrency.Group, "pages-${{ github.event_name }}-${{ github.ref }}"; got != want {
		t.Fatalf("docs concurrency group = %q, want %q", got, want)
	}
}

func TestDocsWorkflowPagesStepsRunOnlyForMainNonPullRequestBuilds(t *testing.T) {
	workflow := loadDocsWorkflow(t)
	build, ok := workflow.Jobs["build"]
	if !ok {
		t.Fatal("docs workflow has no build job")
	}

	conditions := make(map[string]string)
	for _, step := range build.Steps {
		conditions[step.Uses] = step.If
	}
	for _, action := range []string{"actions/configure-pages@v5", "actions/upload-pages-artifact@v3"} {
		condition, ok := conditions[action]
		if !ok {
			t.Fatalf("docs build has no %s step", action)
		}
		for _, tc := range []struct {
			name    string
			context workflowConditionContext
			wantRun bool
		}{
			{name: "pull request", context: workflowConditionContext{EventName: "pull_request", Ref: "refs/pull/4/merge"}},
			{name: "main push", context: workflowConditionContext{EventName: "push", Ref: "refs/heads/main"}, wantRun: true},
			{name: "main dispatch", context: workflowConditionContext{EventName: "workflow_dispatch", Ref: "refs/heads/main"}, wantRun: true},
			{name: "non-main dispatch", context: workflowConditionContext{EventName: "workflow_dispatch", Ref: "refs/heads/fix/codex-native-permissions"}},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				got, err := evaluateWorkflowCondition(condition, tc.context)
				if err != nil {
					t.Fatalf("evaluate condition %q: %v", condition, err)
				}
				if got != tc.wantRun {
					t.Fatalf("step runs = %t, want %t", got, tc.wantRun)
				}
			})
		}
	}
}

func TestDocsWorkflowPreservesBuildTriggersAndMainDeployment(t *testing.T) {
	workflow := loadDocsWorkflow(t)
	for _, event := range []string{"pull_request", "push", "workflow_dispatch"} {
		if _, ok := workflow.On[event]; !ok {
			t.Errorf("docs workflow is missing %s trigger", event)
		}
	}
	deploy, ok := workflow.Jobs["deploy"]
	if !ok {
		t.Fatal("docs workflow has no deploy job")
	}
	for _, tc := range []struct {
		name    string
		ref     string
		wantRun bool
	}{
		{name: "main", ref: "refs/heads/main", wantRun: true},
		{name: "non-main", ref: "refs/heads/fix/codex-native-permissions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateWorkflowCondition(deploy.If, workflowConditionContext{Ref: tc.ref})
			if err != nil {
				t.Fatalf("evaluate deploy condition: %v", err)
			}
			if got != tc.wantRun {
				t.Fatalf("deploy runs = %t, want %t", got, tc.wantRun)
			}
		})
	}
}

func TestCIWorkflowPreservesPullRequestFiltersAndAllowsManualDispatch(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	var workflow struct {
		On map[string]struct {
			Branches    []string `yaml:"branches"`
			PathsIgnore []string `yaml:"paths-ignore"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("parse CI workflow: %v", err)
	}
	if _, ok := workflow.On["workflow_dispatch"]; !ok {
		t.Fatal("CI workflow must allow manual dispatch")
	}
	pullRequest, ok := workflow.On["pull_request"]
	if !ok {
		t.Fatal("CI workflow must retain its pull_request trigger")
	}
	if len(pullRequest.Branches) != 1 || pullRequest.Branches[0] != "main" {
		t.Fatalf("pull_request branches = %v, want [main]", pullRequest.Branches)
	}
	wantIgnored := map[string]bool{".release-please-manifest.json": true, "CHANGELOG.md": true}
	if len(pullRequest.PathsIgnore) != len(wantIgnored) {
		t.Fatalf("pull_request paths-ignore = %v, want existing release output exclusions", pullRequest.PathsIgnore)
	}
	for _, path := range pullRequest.PathsIgnore {
		if !wantIgnored[path] {
			t.Errorf("unexpected pull_request paths-ignore entry %q", path)
		}
	}
}
