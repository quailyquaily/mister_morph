package releaseworkflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsSigningWorkflowContract(t *testing.T) {
	workflow := readRepoFile(t, ".github", "workflows", "windows-signing.yml")

	required := []string{
		"workflow_dispatch:",
		"tag:",
		"ref: ${{ inputs.tag }}",
		"RELEASE_TAG: ${{ inputs.tag }}",
		"runs-on: windows-2022",
		"secrets.ES_USERNAME",
		"secrets.ES_PASSWORD",
		"secrets.ES_CREDENTIAL_ID",
		"secrets.ES_TOTP_SECRET",
		"SSLcom/esigner-codesign@cf5f6c1d38ad10f47e3ed9aca873f429b1a8d85b",
		"command: batch_sign",
		"MisterMorph.exe",
		"mistermorphc.exe",
		"mistermorph-amd64.exe",
		"mistermorph-arm64.exe",
		"signtool verify /pa /all /v /tw",
		"Generate Windows update manifest",
		"gh release upload",
	}
	for _, token := range required {
		if !strings.Contains(workflow, token) {
			t.Errorf("windows signing workflow missing %q", token)
		}
	}

	assertOrdered(t, workflow,
		"command: batch_sign",
		"signtool verify /pa /all /v /tw",
		"Package Windows release assets",
		"Upload Windows release assets",
		"Generate Windows update manifest",
	)

	for _, token := range []string{"workflow_call:", "\n  push:"} {
		if strings.Contains(workflow, token) {
			t.Errorf("manual Windows workflow must not contain %q", token)
		}
	}
}

func TestAutomaticReleaseExcludesUnsignedWindowsArtifacts(t *testing.T) {
	releaseWorkflow := readRepoFile(t, ".github", "workflows", "release.yml")
	goReleaserConfig := readRepoFile(t, ".goreleaser.yaml")

	for _, token := range []string{
		"WINDOWS_CERTIFICATE_BASE64",
		"WINDOWS_CERTIFICATE_PASSWORD",
		"label: windows-amd64",
		"windows-signing:",
		"uses: ./.github/workflows/windows-signing.yml",
	} {
		if strings.Contains(releaseWorkflow, token) {
			t.Errorf("release workflow still contains obsolete Windows path %q", token)
		}
	}

	if strings.Contains(goReleaserConfig, "- windows") {
		t.Fatal("GoReleaser must not publish unsigned Windows archives")
	}
}

func assertOrdered(t *testing.T, text string, tokens ...string) {
	t.Helper()
	previous := -1
	for _, token := range tokens {
		index := strings.Index(text, token)
		if index < 0 {
			t.Fatalf("missing ordered token %q", token)
		}
		if index <= previous {
			t.Fatalf("token %q appears out of order", token)
		}
		previous = index
	}
}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	path := filepath.Join(append([]string{repoRoot}, parts...)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(content)
}
