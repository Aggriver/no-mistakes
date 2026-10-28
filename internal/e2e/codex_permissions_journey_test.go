//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/types"
)

// TestCodexExplicitPermissionsJourney proves the operator-facing configuration
// contract through the real config loader, pipeline, and Codex subprocess
// boundary. The review conversation forces the second reviewer call through
// `codex exec resume`, whose narrower flag surface is why permission profiles
// must be passed as config overrides.
func TestCodexExplicitPermissionsJourney(t *testing.T) {
	const permissionOverride = `default_permissions="project-gate"`
	h := NewHarness(t, SetupOpts{
		Agent:    "codex",
		Scenario: reviewConversationScenario(t),
		GlobalConfigExtra: "agent_args_override:\n" +
			"  codex: [-c, '" + permissionOverride + "']\n",
	})
	pushMainRepoConfig(t, h, trustedRepoConfigWithReviewConversation)

	if out, err := h.Run("init"); err != nil {
		t.Fatalf("nm init: %v\n%s", err, out)
	}
	branch := "feature/codex-explicit-permissions"
	h.CommitChange(branch, "feature.txt", "flag = true\n", "add permission-gated feature")
	fw := h.AddWorktree(branch)
	h.PushToGate(branch)
	gated := waitForStepStatus(t, h, branch, types.StepReview, types.StepStatusAwaitingApproval, 90*time.Second)
	if gated == nil {
		t.Fatal("review step never parked for the question")
	}

	answerOut, err := h.RunInDir(fw, "axi", "answer", "--question", "q1", "--answer", conversationAnswerText, "--by", conversationAnsweredBy)
	if err != nil {
		t.Fatalf("axi answer: %v\n%s", err, answerOut)
	}
	for _, want := range []string{"answered: true", "open_questions: 0", "reviewer_resumed: true"} {
		if !strings.Contains(answerOut, want) {
			t.Fatalf("axi answer output missing %q:\n%s", want, answerOut)
		}
	}
	completed := h.WaitForRun(branch, 120*time.Second)
	if completed.Status != types.RunCompleted {
		t.Fatalf("run status = %s, want completed (error=%v)", completed.Status, deref(completed.Error))
	}

	var sawFresh, sawResume bool
	for _, inv := range h.AgentInvocations() {
		if inv.Agent != "codex" {
			continue
		}
		if !hasExactArg(inv.Args, permissionOverride) {
			t.Fatalf("Codex invocation lost explicit permission override: %v", inv.Args)
		}
		if hasExactArg(inv.Args, "--dangerously-bypass-approvals-and-sandbox") {
			t.Fatalf("Codex invocation combined explicit permissions with bypass: %v", inv.Args)
		}
		if len(inv.Args) > 1 && inv.Args[0] == "exec" && inv.Args[1] == "resume" {
			sawResume = true
		} else if len(inv.Args) > 0 && inv.Args[0] == "exec" {
			sawFresh = true
		}
	}
	if !sawFresh || !sawResume {
		t.Fatalf("Codex calls fresh=%t resume=%t, want both; invocations=%+v", sawFresh, sawResume, h.AgentInvocations())
	}
	if !strings.Contains(answerOut, "outcome: passed") {
		t.Fatalf("answer did not follow the permission-configured run to success:\n%s", answerOut)
	}
}

// TestCodexNestedPermissionKeyKeepsDefaultBypass is the adversarial boundary:
// only the three documented top-level keys suppress the existing default.
func TestCodexNestedPermissionKeyKeepsDefaultBypass(t *testing.T) {
	const nestedOverride = `profiles.project-gate.default_permissions="project-gate"`
	h := NewHarness(t, SetupOpts{
		Agent:    "codex",
		Scenario: cleanReviewScenario(t),
		GlobalConfigExtra: "agent_args_override:\n" +
			"  codex: [-c, '" + nestedOverride + "']\n",
	})
	if out, err := h.Run("init"); err != nil {
		t.Fatalf("nm init: %v\n%s", err, out)
	}
	branch := "feature/codex-nested-permission"
	h.CommitChange(branch, "nested.txt", "nested config\n", "exercise nested Codex config")
	h.PushToGate(branch)
	completed := h.WaitForRun(branch, 120*time.Second)
	if completed.Status != types.RunCompleted {
		t.Fatalf("run status = %s, want completed (error=%v)", completed.Status, deref(completed.Error))
	}

	invs := h.AgentInvocations()
	if len(invs) == 0 {
		t.Fatal("pipeline made no agent invocations")
	}
	for _, inv := range invs {
		if !hasExactArg(inv.Args, nestedOverride) {
			t.Fatalf("Codex invocation lost nested config override: %v", inv.Args)
		}
		if !hasExactArg(inv.Args, "--dangerously-bypass-approvals-and-sandbox") {
			t.Fatalf("nested key accidentally suppressed default bypass: %v", inv.Args)
		}
	}
}

// TestCodexExecutionPolicySpellingsJourney drives the other Codex-supported
// config spellings through global config loading and a real pipeline run. The
// separate short form is covered above because it additionally needs resume.
func TestCodexExecutionPolicySpellingsJourney(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"long-separate", []string{"--config", `default_permissions="project-gate"`}},
		{"long-equals", []string{`--config=default_permissions="project-gate"`}},
		{"short-attached", []string{`-cdefault_permissions="project-gate"`}},
		{"short-equals", []string{`-c=default_permissions="project-gate"`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quoted := make([]string, 0, len(tc.args))
			for _, arg := range tc.args {
				quoted = append(quoted, strconv.Quote(arg))
			}
			h := NewHarness(t, SetupOpts{
				Agent:             "codex",
				Scenario:          cleanReviewScenario(t),
				GlobalConfigExtra: "agent_args_override:\n  codex: [" + strings.Join(quoted, ", ") + "]\n",
			})
			if out, err := h.Run("init"); err != nil {
				t.Fatalf("nm init: %v\n%s", err, out)
			}
			branch := "feature/codex-config-" + tc.name
			h.CommitChange(branch, tc.name+".txt", "configured\n", "exercise Codex "+tc.name)
			h.PushToGate(branch)
			completed := h.WaitForRun(branch, 120*time.Second)
			if completed.Status != types.RunCompleted {
				t.Fatalf("run status = %s, want completed (error=%v)", completed.Status, deref(completed.Error))
			}
			invs := h.AgentInvocations()
			if len(invs) == 0 {
				t.Fatal("pipeline made no agent invocations")
			}
			for _, inv := range invs {
				for _, want := range tc.args {
					if !hasExactArg(inv.Args, want) {
						t.Fatalf("Codex invocation lost config argument %q: %v", want, inv.Args)
					}
				}
				if hasExactArg(inv.Args, "--dangerously-bypass-approvals-and-sandbox") {
					t.Fatalf("%s did not suppress default bypass: %v", tc.name, inv.Args)
				}
			}
		})
	}
}

func hasExactArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
