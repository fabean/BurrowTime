# Distribution

BurrowTime uses GitHub Releases as the canonical source for versioned binaries.
Package-manager entries should download or build from an immutable tagged
release rather than creating an independent release process.

```text
version tag
    └── GitHub Actions
          ├── tests
          └── GoReleaser
                ├── Linux archives (amd64, arm64)
                ├── macOS archives (amd64, arm64)
                ├── Windows archives (amd64, arm64)
                └── SHA-256 checksums
                          ├── AUR PKGBUILD
                          ├── Homebrew tap
                          └── Scoop bucket
```

## GitHub Releases

The release workflow is defined in
[`release.yml`](../.github/workflows/release.yml), and the artifact matrix is
defined in [`.goreleaser.yaml`](../.goreleaser.yaml).

Each archive contains both executables:

- `burrowtime`, the normal CLI and interactive TUI;
- `watson`, the optional compatibility entry point.

The only repository permission used by the workflow is `contents: write`,
which allows its `GITHUB_TOKEN` to create a release and upload assets.

Before the first release:

1. Create the public `github.com/josh/burrowtime` repository.
2. Add it as this checkout's `origin` and push the default branch.
3. Confirm GitHub Actions are enabled.
4. Run a local snapshot and inspect `dist/`:

   ```bash
   goreleaser release --snapshot --clean
   ```

5. Create and push an annotated semantic-version tag:

   ```bash
   git tag -a v0.1.0 -m "BurrowTime v0.1.0"
   git push origin v0.1.0
   ```

Pushing the tag runs tests and publishes the release. Prerelease tags such as
`v0.2.0-rc.1` are marked as prereleases automatically.

## Arch User Repository

The AUR stores `PKGBUILD` recipes, not binary packages. Because
`burrowtime-bin` would consume the prebuilt GitHub artifacts while source is
available, Arch's naming rules require the `-bin` suffix. A separate
`burrowtime` recipe could build a tagged source release with Go.

Recommended first package: **`burrowtime-bin`**.

One-time setup:

1. Create an AUR account and add an SSH public key.
2. Check that neither `burrowtime` nor `burrowtime-bin` already exists.
3. Create and test the recipe locally with `makepkg -si`.
4. Generate `.SRCINFO` with `makepkg --printsrcinfo > .SRCINFO`.
5. Push `PKGBUILD` and `.SRCINFO` to
   `ssh://aur@aur.archlinux.org/burrowtime-bin.git`.

After the first package is accepted, GoReleaser's `aurs` publisher can update
it on every stable tag. It needs:

- an `aurs` entry for `burrowtime-bin` in `.goreleaser.yaml`;
- a package step that installs both executables and `LICENSE`;
- an unencrypted deploy key stored as the `AUR_KEY` GitHub Actions secret;
- prerelease uploads disabled.

Do not add that publisher before the AUR repository and secret exist: doing so
would turn an otherwise valid GitHub release into a failed release job.

References:

- [AUR submission guidelines](https://wiki.archlinux.org/title/AUR_submission_guidelines)
- [Creating packages](https://wiki.archlinux.org/title/Creating_packages)
- [GoReleaser AUR publisher](https://goreleaser.com/customization/publish/aur/)

## Homebrew

Use a separate public repository named `josh/homebrew-tap`. Current GoReleaser
uses `homebrew_casks` for prebuilt command-line assets; the older `brews`
integration is deprecated.

One-time setup:

1. Create the tap with `brew tap-new josh/tap` and publish it as
   `josh/homebrew-tap`.
2. Add a `homebrew_casks` entry that consumes the `release` archive and installs
   both `burrowtime` and `watson`.
3. Create a fine-grained token with write access to the tap repository and add
   it to this repository as `GH_PAT`.
4. Pass `GH_PAT` to GoReleaser for cross-repository publishing.
5. Test installation on both Apple Silicon and Intel macOS before advertising:

   ```bash
   brew install --cask josh/tap/burrowtime
   burrowtime --version
   ```

The workflow's default `GITHUB_TOKEN` cannot write to a separate tap
repository, which is why the additional token is required.

References:

- [Homebrew tap documentation](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap)
- [GoReleaser Homebrew casks](https://goreleaser.com/customization/publish/homebrew_casks/)

## Windows and other package managers

Scoop is the easiest next Windows target: create a bucket repository and let
GoReleaser update a manifest that points at the Windows ZIP archive. Winget can
follow once releases are stable, but it requires maintaining and submitting
versioned manifests to the community package repository.

Potential rollout order:

1. GitHub Releases and `go install`;
2. `burrowtime-bin` on AUR;
3. `josh/homebrew-tap` for macOS and Linuxbrew;
4. a Scoop bucket for Windows;
5. official/community repositories after the project has users and stable
   releases.

Reference: [GoReleaser Scoop publisher](https://goreleaser.com/customization/publish/scoop/).

## Release checklist

- Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- Run and inspect a GoReleaser snapshot.
- Verify the version prints correctly from both release binaries.
- Review generated release notes.
- Download one published archive and verify it against `checksums.txt`.
- Test the TUI in a real terminal on Linux and macOS.
- Update package-manager recipes only after the GitHub release is available.
