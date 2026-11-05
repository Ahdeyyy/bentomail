# Bentomail

Bentomail is a native desktop email client for any standards-compliant IMAP/SMTP provider. It is written in Go with the [MyGo](https://github.com/egoist/mygo) native UI toolkit and runs on Windows, Linux, and macOS.

Bentomail is local-first: mail, drafts, attachments, tags, rules, and the full-text search index are stored in a SQLite database on your computer, so downloaded mail stays readable offline. Changes made while offline are queued and sent to the server when you reconnect. Bentomail has no cloud service of its own; it talks directly to your mail servers.

> **Platform note:** Saved credentials are encrypted with Windows DPAPI. Account passwords and OAuth tokens cannot yet be saved on Linux or macOS.

## Features

### Accounts and synchronization

- Multiple accounts with a unified inbox, per-account views, a default sending account, and account enable/disable.
- Presets for Google and Microsoft, manual setup for other providers, and a **Test connection** button.
- Google and Microsoft OAuth 2.0 sign-in in the system browser using PKCE, with automatic token refresh.
- Sending aliases with their own display names, Reply-To addresses, signatures, and a default identity.
- Full or recent-range synchronization of up to three accounts at once, with the Inbox and newer mail fetched first. Sync can be canceled, and finished batches are kept.
- IMAP IDLE push notifications, configurable polling, and CONDSTORE support for fast flag updates.
- Offline flag and move changes, saved locally and queued for the server.
- Mail removed from the server stays in a **Local archive** and is not deleted from your computer.

### Reading and organizing

- Threaded or individual message lists, a conversation view, and HTML, plain-text, and source views.
- Configurable reading-pane placement, multi-select, bulk actions, drag to a folder or tag, archive, junk, trash, and undo.
- Colored tags, editable categories, saved searches, local mail rules with match previews, snooze, and follow-up reminders.
- Full-text search with operators:

  ```text
  from:alex@example.com subject:proposal
  tag:"Client A" is:unread has:attachment
  "exact phrase" -subject:newsletter
  after:2026-10-01 before:2026-11-01 attachment:pdf
  category:Finance folder:INBOX
  ```

### Composing and sending

- A native rich-text editor with headings, lists, quotes, fonts, colors, alignment, links, undo/redo, and rich clipboard support. Formatted mail always includes a plain-text alternative.
- Recipient chips with address suggestions, To/Cc/Bcc, reply, reply-all, forward, and forward as attachment.
- Several open drafts at once, autosave and recovery, signatures, and reusable templates.
- Attachments by file picker or drag and drop, inline images, image previews, and missing-attachment reminders. Outgoing attachments can total up to 50 MB.
- Delayed and scheduled sending with an editable Outbox. Bentomail never automatically resends a message whose delivery is uncertain.

### Privacy and security

- HTML sanitization with an allowlist and a strict Content Security Policy. Scripts and remote styles are removed.
- Remote images are blocked by default, with global and trusted-sender options.
- Links ask for confirmation before opening in your browser.
- TLS certificate checks are required, and a failed TLS connection never falls back to plain text. Unencrypted connections are only allowed to localhost.
- Credentials, refresh tokens, and OAuth client secrets are encrypted with Windows DPAPI.

### Desktop experience

- Light and dark themes, accent palettes, adjustable translucency (Acrylic blur on Windows 11), comfortable or compact density, and reading font options.
- A resizable sidebar that collapses to an icon rail, resizable panes, and a remembered window layout.
- Native menus, dialogs, desktop notifications, quiet hours, unread badges, and optional system-tray operation.
- Keyboard shortcuts and keyboard-navigable context menus.
- Animations that follow the system's reduced-motion setting.
- Local contacts collected automatically from the people you write to.
- Backup and restore, `.eml` import and export, and search-index rebuilding.
- Built-in signed auto-updates from GitHub Releases.

## Installing

Download the latest build from [GitHub Releases](https://github.com/Ahdeyyy/bentomail/releases):

- **Windows:** installer or app archive (x64 and ARM64). The installer is unsigned.
- **Linux:** `.deb` package or app archive (x64 and ARM64).
- **macOS:** universal `.dmg` or app archive (Intel and Apple Silicon). The app is signed ad hoc and is not notarized.

## Running locally

### Prerequisites

- [Go](https://go.dev/dl/), at the version in [`go.mod`](go.mod). CGO is not required.
- Git.

All other tools, including the MyGo CLI, are Go module dependencies and are downloaded automatically.

### Run from source

```sh
git clone https://github.com/Ahdeyyy/bentomail.git
cd bentomail
go run .
```

By default, data is stored in a `Bentomail` folder in your user config directory (`%APPDATA%\Bentomail` on Windows). Use `--data` to choose another location, for example to keep test data separate:

```sh
go run . --data ./scratch/data
```

### Set up an account

1. Open **Settings → Accounts**.
2. Enter your display name and email address, then fill in the IMAP and SMTP hosts, security modes, ports, and credentials. The Google and Microsoft presets fill in the server details for you.
3. Click **Test connection**, then **Save account**.

#### Google and Microsoft OAuth

Bentomail does not include a production OAuth app registration, so you need to provide your own client ID. Select `oauth2` in the account settings, choose a provider, and enter the client ID. Google may also give you a client secret. Then click **Sign in securely**.

- **Google:** Create a Desktop OAuth client and add your users to the consent screen. Bentomail requests the `https://mail.google.com/` scope. See [Google's desktop OAuth documentation](https://developers.google.com/identity/protocols/oauth2/native-app).
- **Microsoft:** Register a public/native application with delegated IMAP and SMTP permissions and the redirect URI `http://localhost/oauth/callback`. See [Microsoft's IMAP/SMTP OAuth documentation](https://learn.microsoft.com/en-us/exchange/client-developer/legacy-protocols/how-to-authenticate-an-imap-pop-smtp-application-by-using-oauth).

## Building

Build for the current platform:

```sh
go tool mygo build
```

The output goes to the `build/` directory, for example `build/windows-amd64/bentomail.exe` along with a `bentomail Setup` installer on Windows.

On Windows, `./build.ps1` runs the tests and MyGo's UI checks and then builds the app and installer:

```powershell
./build.ps1
```

Cross-compile every desktop target:

```sh
go tool mygo build -platform windows/amd64,windows/arm64,linux/amd64,linux/arm64,darwin/universal
```

Building macOS disk images and signing the app requires macOS. Windows installers need NSIS, which MyGo downloads automatically on Windows. To produce a signed update archive locally, set `MYGO_UPDATER_PRIVATE_KEY` to the update signing key. Development builds report that updates are unavailable.

### Tests and checks

```powershell
$env:CGO_ENABLED = '0'
go test ./...
go tool mygo vet .
go vet ./...
```

Credential-storage tests use DPAPI, so the full suite runs on Windows. To export UI screenshots that use synthetic test mail:

```powershell
$env:BENTOMAIL_SCREENSHOTS = 'scratch/screenshots'
go test . -run TestRenderWorkspace -count=1
```

### Brand assets

App icons are checked in. If the artwork changes, regenerate the PNG and ICO files with `go run ./cmd/brand-assets`. The original logo concepts are in `resources/logo-concepts/`, and `node scripts/create_logos.mjs` regenerates them.

### Continuous integration and releases

GitHub Actions ([`.github/workflows/build.yml`](.github/workflows/build.yml)) builds every target on Windows, Linux, and macOS runners for each push, pull request, and manual run. Successful runs on the default branch publish a GitHub Release tagged `bentomail-v<version>`. The version is the highest existing patch version plus one, and the minimum version comes from `mygo.json`. Update archives are signed with the `MYGO_UPDATER_PRIVATE_KEY` Actions secret.

## How Bentomail handles your data

- `mail.sqlite` stores messages, drafts, metadata, queued changes, and the search index. `secrets.dat` stores the DPAPI-encrypted credentials. Bentomail does not encrypt mail at rest itself, so your operating system's account and disk protection apply.
- Bentomail does not delete mail from the server to save local space. Removing an account keeps its downloaded mail in Local archive and deletes its credentials.
- Backups include mail and preferences but leave out credentials and pending server actions.
- Scheduled sends, rules, snooze, reminders, and IDLE only run while Bentomail is open. Turn on tray operation to keep it running after you close the window.
- If an SMTP connection drops after the message has been sent to the server, the draft is marked **uncertain** and is not retried automatically. Check your Sent folder before you retry it.
- Moving mail on the server requires the IMAP `MOVE` extension. Tags, rules, snooze, follow-ups, and templates are stored only on your computer.

## Acknowledgements

Bentomail is built on the following open-source projects. Thank you to their authors and contributors.

### Direct dependencies

| Project | Used for | License |
| --- | --- | --- |
| [MyGo](https://github.com/egoist/mygo) | Native cross-platform UI, packaging, installers, and auto-updates | MIT |
| [go-imap](https://github.com/emersion/go-imap) | IMAP client | MIT |
| [go-message](https://github.com/emersion/go-message) | MIME message parsing and generation | MIT |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | CGO-free SQLite database, a translation of [SQLite](https://sqlite.org) (public domain) | BSD-3-Clause |
| [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) | HTML parsing for sanitization | BSD-3-Clause |

### Indirect dependencies

| Project | License |
| --- | --- |
| [go-sasl](https://github.com/emersion/go-sasl) | MIT |
| [go-text/typesetting](https://github.com/go-text/typesetting) | Unlicense or BSD-3-Clause |
| [purego](https://github.com/ebitengine/purego) | Apache-2.0 |
| [go-humanize](https://github.com/dustin/go-humanize) | MIT |
| [google/uuid](https://github.com/google/uuid) | BSD-3-Clause |
| [go-isatty](https://github.com/mattn/go-isatty) | MIT |
| [go-strftime](https://github.com/ncruces/go-strftime) | MIT |
| [bigfft](https://github.com/remyoudompheng/bigfft) | BSD-3-Clause |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image), [x/sys](https://pkg.go.dev/golang.org/x/sys), [x/text](https://pkg.go.dev/golang.org/x/text) | BSD-3-Clause |
| [modernc.org/libc](https://gitlab.com/cznic/libc), [mathutil](https://gitlab.com/cznic/mathutil), [memory](https://gitlab.com/cznic/memory) | BSD-3-Clause |

Exact versions are pinned in [`go.mod`](go.mod) and [`go.sum`](go.sum).

### Fonts

The interface uses [Martian Grotesk](https://github.com/evilmartians/grotesk) by Evil Martians, bundled under the SIL Open Font License 1.1. See [`resources/fonts/OFL.txt`](resources/fonts/OFL.txt).

## License

Bentomail is released under the [MIT License](LICENSE). Third-party components keep their own licenses, listed above.
