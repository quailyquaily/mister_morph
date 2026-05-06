# OS Signing

This document describes release signing for desktop builds distributed outside app stores.

Signing does not make a build safe by itself. It gives the operating system a publisher identity, protects the artifact from unnoticed modification, and lets OS reputation systems make a better decision.

## Release Assets

The tag release workflow builds these desktop assets:

- macOS: `mistermorph-desktop-darwin-arm64.dmg`
- Windows: `mistermorph-desktop-windows-amd64.zip`
- Linux: `mistermorph-desktop-linux-amd64.AppImage`

macOS signs and notarizes the `.app` and `.dmg`.

Windows should sign the executables inside the zip:

- `MisterMorph.exe`
- `mistermorphc.exe`

The zip itself does not need an Authenticode signature. If we later ship an installer, sign the installer too.

Linux AppImage signing is not part of the current release path. There is no Gatekeeper-equivalent step in the current Linux packaging flow.

## macOS

macOS distribution outside the Mac App Store needs Developer ID signing and notarization.

Gatekeeper checks the Developer ID signature and the notarization ticket when users launch the app.

### Required Apple Setup

The Apple Developer account owner or an authorized admin must prepare:

- Apple Developer Program membership
- Developer ID Application certificate
- `.p12` export containing the Developer ID Application certificate and private key
- App-specific password for the Apple ID used by `notarytool`
- Apple Developer Team ID

Use the current Apple Developer certificate path for modern Xcode, not the previous Sub-CA path.

### GitHub Secrets

The release workflow expects these secrets for the macOS DMG job:

- `APPLE_CERTIFICATE_BASE64`: base64-encoded `.p12` containing the Developer ID Application certificate and private key
- `APPLE_CERTIFICATE_PASSWORD`: password for the `.p12`
- `APPLE_CODESIGN_IDENTITY`: full identity, for example `Developer ID Application: Example Inc (TEAMID1234)`
- `APPLE_ID`: Apple ID used for notarization
- `APPLE_TEAM_ID`: Apple Developer Team ID
- `APPLE_APP_PASSWORD`: app-specific password for notarization

Do not commit certificates, passwords, private keys, keychain files, or local export paths.

### CI Flow

The tag release workflow does this on the macOS runner:

1. Builds the desktop binary.
2. Builds the bundled backend binary.
3. Validates that all macOS signing secrets exist.
4. Imports the `.p12` into a temporary keychain.
5. Signs the bundled backend with `codesign --options runtime --timestamp`.
6. Signs the desktop app binary with `codesign --options runtime --timestamp`.
7. Signs the `.app` bundle.
8. Verifies the `.app` signature.
9. Creates the DMG.
10. Signs the DMG.
11. Submits the DMG to Apple with `xcrun notarytool submit --wait`.
12. Staples the notarization ticket to the DMG and the `.app`.
13. Uploads the release assets.

The packaging script is:

```bash
desktop/wails/packaging/package-darwin.sh
```

If `CODESIGN_IDENTITY` is not set, the script uses ad hoc signing for local test distribution. Ad hoc builds are not release builds and may require testers to manually bypass Gatekeeper.

### Verification

Useful checks on macOS:

```bash
codesign --verify --deep --strict --verbose=2 MisterMorph.app
spctl --assess --type execute --verbose MisterMorph.app
xcrun stapler validate MisterMorph.app
xcrun stapler validate mistermorph-desktop-darwin-arm64.dmg
```

Use `notarytool log` when notarization fails. Most failures are caused by a missing hardened runtime signature, an unsigned nested executable, a wrong Developer ID certificate type, or a broken timestamp.

## Windows

Windows does not have Gatekeeper. The relevant systems are Microsoft Defender SmartScreen and Smart App Control.

Unsigned downloads are treated as higher risk. Signed downloads show a verified publisher, and signed binaries are less likely to be blocked by strict policy. A new signed file can still show a SmartScreen warning until that file hash gains reputation.

### What Must Be Signed

Sign every shipped Windows executable before packaging:

- `dist/MisterMorph.exe`
- `dist/mistermorphc.exe`

If we later produce an MSI, MSIX, or NSIS installer, sign the installer after it is built. Keep the inner executables signed as well.

Always timestamp signatures. Timestamping keeps an already-signed release valid after the signing certificate expires.

### Current PFX Path

The release workflow contains a PFX-based `signtool` step for Windows.

It expects:

- `WINDOWS_CERTIFICATE_BASE64`: base64-encoded `.pfx` or `.p12` code signing certificate with private key
- `WINDOWS_CERTIFICATE_PASSWORD`: password for the certificate file

That path is acceptable if we already have a traditional code signing certificate. It is less convenient for CI because the private key is exported into GitHub Actions secrets.

### Recommended Trusted Signing Path

For new setup, prefer Microsoft Trusted Signing, also called Azure Artifact Signing in current Microsoft docs.

The account owner must prepare these Azure resources:

- Microsoft Entra tenant
- Trusted Signing account
- identity validation
- Public Trust certificate profile
- GitHub Actions identity with the certificate profile signer role

Use OpenID Connect for GitHub Actions when possible. Client-secret auth also works, but it adds another long-lived secret.

Recommended GitHub variables or secrets:

- `AZURE_TENANT_ID`
- `AZURE_CLIENT_ID`
- `AZURE_CLIENT_SECRET` only if not using OIDC
- `AZURE_TRUSTED_SIGNING_ENDPOINT`, for example `https://eus.codesigning.azure.net/`
- `AZURE_TRUSTED_SIGNING_ACCOUNT_NAME`
- `AZURE_TRUSTED_SIGNING_CERTIFICATE_PROFILE_NAME`

The CI step should run on a Windows runner after both Windows executables are built and before the zip is created. It should sign both `.exe` files with:

- file digest: `SHA256`
- timestamp URL: `http://timestamp.acs.microsoft.com`
- timestamp digest: `SHA256`

The GitHub Actions job also needs:

```yaml
permissions:
  contents: write
  id-token: write
```

`id-token: write` is only required for OIDC. It is not needed for the existing PFX flow.

### Verification

Useful checks on Windows:

```powershell
Get-AuthenticodeSignature .\dist\MisterMorph.exe
Get-AuthenticodeSignature .\dist\mistermorphc.exe
signtool verify /pa /v .\dist\MisterMorph.exe
signtool verify /pa /v .\dist\mistermorphc.exe
```

Expected result:

- status is valid
- signer is the expected publisher
- timestamp exists

SmartScreen reputation is separate from signature validity. A valid signature can still show an early-download reputation prompt for new releases.

## Operator Checklist

Before enabling release signing:

- macOS Developer ID certificate exists and can be exported with the private key.
- Apple notarization credentials are stored in GitHub Secrets.
- Windows signing path is chosen: PFX or Trusted Signing.
- Windows executables are signed before the zip is created.
- No signing key material is committed to the repository.
- A tagged release has been tested on a clean machine for each signed OS.

## References

- Apple Developer: Developer ID certificates
  `https://developer.apple.com/help/account/create-certificates/create-developer-id-certificates/`
- Apple Developer: Notarizing macOS software before distribution
  `https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution`
- Microsoft Learn: SmartScreen reputation for Windows app developers
  `https://learn.microsoft.com/windows/apps/package-and-deploy/smartscreen-reputation`
- Microsoft Learn: Artifact Signing quickstart
  `https://learn.microsoft.com/azure/trusted-signing/quickstart`
- Microsoft Learn: Set up signing integrations to use Artifact Signing
  `https://learn.microsoft.com/azure/trusted-signing/how-to-signing-integrations`
- Azure Trusted Signing GitHub Action
  `https://github.com/Azure/trusted-signing-action`
