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
		"actions/checkout@v5",
		"actions/setup-go@v6",
		"pnpm/action-setup@v6",
		"actions/setup-node@v5",
		"secrets.ES_USERNAME",
		"secrets.ES_PASSWORD",
		"secrets.ES_CREDENTIAL_ID",
		"secrets.ES_TOTP_SECRET",
		"actions/setup-java@v5",
		"CodeSignTool/releases/download/v1.3.2/CodeSignTool-v1.3.2.zip",
		"f14b1e1ef14bfa1fd00279c363aab0debbf5dcfba0e4bcdce5d22bb771de0e3a",
		"[System.Diagnostics.ProcessStartInfo]::new()",
		"$processInfo.ArgumentList.Add($argument)",
		"$processInfo.RedirectStandardOutput = $true",
		`"scan_code"`,
		"code object is not a malware. You can proceed signing this code object",
		`"batch_sign"`,
		"Batch sign command executed successfully.",
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
		"Download CodeSignTool",
		"$processInfo.ArgumentList.Add($argument)",
		"foreach ($file in $unsignedFiles)",
		`"scan_code"`,
		`"batch_sign"`,
		`$signedFiles = @(Get-ChildItem $signedDir -File -Filter "*.exe")`,
		"signtool verify /pa /all /v /tw",
		"Package Windows release assets",
		"Upload Windows release assets",
		"Generate Windows update manifest",
	)

	for _, token := range []string{
		"workflow_call:",
		"\n  push:",
		"SSLcom/esigner-codesign@",
		"CodeSignTool.bat",
		"ACTIONS_ALLOW_USE_UNSECURE_NODE_VERSION",
	} {
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

func TestDesktopPackagesRegisterMisterMorphApplicationID(t *testing.T) {
	for _, file := range [][]string{
		{"desktop", "wails", "packaging", "package-darwin.sh"},
		{"desktop", "wails", "packaging", "package-linux-appimage.sh"},
		{"desktop", "wails", "packaging", "package-linux-deb.sh"},
		{"desktop", "wails", "packaging", "windows", "wails.exe.manifest"},
	} {
		content := readRepoFile(t, file...)
		if !strings.Contains(content, "com.mistermorph") {
			t.Errorf("%s does not register com.mistermorph", filepath.Join(file...))
		}
	}

	for _, file := range [][]string{
		{"desktop", "wails", "packaging", "package-linux-appimage.sh"},
		{"desktop", "wails", "packaging", "package-linux-deb.sh"},
	} {
		content := readRepoFile(t, file...)
		if !strings.Contains(content, "APPLICATION_ID") || !strings.Contains(content, "Icon=${APPLICATION_ID}") {
			t.Errorf("%s does not associate the application ID with its icon", filepath.Join(file...))
		}
	}
}

func TestDarwinPackageBuildsStyledDMG(t *testing.T) {
	script := readRepoFile(t, "desktop", "wails", "packaging", "package-darwin.sh")

	for _, token := range []string{
		"DMG_BACKGROUND_SOURCE",
		"DMG_STAGING_DIR",
		`DMG_MOUNT_DIR="/Volumes/${DMG_VOLUME_NAME}"`,
		".background",
		`ln -s "/Applications"`,
		"osascript",
		"if exists disk volumeName then",
		"-format UDRW",
		"hdiutil attach",
		"hdiutil detach",
		"hdiutil convert",
	} {
		if !strings.Contains(script, token) {
			t.Errorf("macOS package script missing %q", token)
		}
	}
	if strings.Contains(script, `-mountpoint "${DMG_MOUNT_DIR}"`) {
		t.Fatal("macOS package script must let Disk Arbitration mount the DMG where Finder can see it")
	}

	assertOrdered(t, script,
		"\t-format UDRW",
		"\nhdiutil attach",
		"\nosascript -",
		"\tif hdiutil detach",
		"\nhdiutil convert",
		`echo "signing DMG`,
	)

	background := readRepoFile(t, "desktop", "wails", "packaging", "dmg-background.svg")
	if !strings.Contains(background, `viewBox="0 0 760 480"`) {
		t.Fatal("DMG background must match the Finder window content size")
	}
	if len(readRepoFile(t, "desktop", "wails", "packaging", "dmg-background.png")) == 0 {
		t.Fatal("DMG background PNG is empty")
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
