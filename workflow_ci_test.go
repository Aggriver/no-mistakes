package main

import (
	"slices"
	"testing"
	"time"
)

func TestCIWorkflowRunsTestsOnAllSupportedDesktopPlatforms(t *testing.T) {
	job := ciTestJob(t)
	got := make(map[string]int)
	for _, row := range job.Strategy.Matrix.Include {
		got[row["os"]]++
	}

	want := map[string]int{
		"ubuntu-latest":  1,
		"macos-latest":   1,
		"windows-latest": 3,
	}
	if len(got) != len(want) {
		t.Fatalf("test matrix operating systems = %v, want %v", got, want)
	}
	for osName, count := range want {
		if got[osName] != count {
			t.Errorf("test matrix rows for %q = %d, want %d", osName, got[osName], count)
		}
	}
}

func TestCIWorkflowUsesRaceTestsOnUnixRunners(t *testing.T) {
	job := ciTestJob(t)
	allCommands := workflowCommandsMatching(job.Steps, func(wfStep) bool { return true })
	var allRaceTests []workflowCommand
	for _, command := range allCommands {
		if command.name == "go" && len(command.args) >= 2 && command.args[0] == "test" && slices.Contains(command.args, "-race") {
			allRaceTests = append(allRaceTests, command)
		}
	}
	if len(allRaceTests) != 2 {
		t.Fatalf("race test commands = %d, want one Linux and one macOS command; normalized commands: %#v", len(allRaceTests), allCommands)
	}

	linuxCommands := workflowCommandsMatching(job.Steps, func(step wfStep) bool {
		return exactRunnerOSCondition(step.If, "==", "Linux")
	})
	macOSCommands := workflowCommandsMatching(job.Steps, func(step wfStep) bool {
		return exactRunnerOSCondition(step.If, "==", "macOS")
	})
	if len(linuxCommands) != 1 || !slices.Equal(linuxCommands[0].args, []string{"test", "-race", "./..."}) {
		t.Fatalf("Linux commands = %#v, want exactly go test -race ./...", linuxCommands)
	}
	if len(macOSCommands) != 1 || !slices.Equal(macOSCommands[0].args, []string{"test", "-race", "-timeout=20m", "./..."}) {
		t.Fatalf("macOS commands = %#v, want exactly go test -race -timeout=20m ./...", macOSCommands)
	}

	goTimeout := goTestTimeout(t, macOSCommands[0])
	jobTimeout := time.Duration(job.TimeoutMinutes) * time.Minute
	if goTimeout >= jobTimeout {
		t.Fatalf("macOS go test -timeout is %s and the job cap is %s; the Go timeout must fire first so a hang produces a goroutine dump", goTimeout, jobTimeout)
	}
}
