# Bentomail

A native IMAP/SMTP mail client built with Go and MyGo 0.5.5. Builds cover Windows and Linux on x64/ARM64, and macOS on Intel/Apple Silicon with a universal app. Mail, drafts, attachments, tags, and the search index live on your computer. Saved-credential encryption currently requires Windows DPAPI; account passwords and OAuth tokens cannot yet be saved on Linux or macOS. The interface retains localmailhost's Martian Grotesk typography and cool indigo light/dark palette.

## Run

Open `build/windows-amd64/bentomail.exe`, or install with the `bentomail Setup` executable in the same directory. The Windows installer is unsigned; in-app update archives are signed separately.

GitHub Releases include Windows installers and app archives for both architectures, Linux `.deb` packages and archives for both architectures, and a universal macOS `.dmg` and app archive. The macOS app is signed ad hoc and is not notarized.

For development:

```powershell
go run .
```

The default data directory is `%APPDATA%\Bentomail`. To use a different directory:

```powershell
go run . --data 'D:\Mail\Bentomail'
```

Choose **Settings → Accounts**. Set your display name and email, then enter the IMAP/SMTP hosts, security modes, ports, and credentials. Use **Test connection**, then **Save account**. Google and Microsoft presets fill their server details. Other standards-compliant providers can be configured manually.

The **display name** is written into the actual MIME From header, e.g. `Alex Morgan <alex@company.com>`. Each account can also have sending aliases with separate display names, Reply-To addresses, signatures, and a default identity. The provider must authorize an alias. Recipients' saved contacts and mail apps can override the name shown on their screens.

## Features

- Multiple accounts, unified inbox, individual account views, default sending account, aliases, account enable/disable, and connection testing.
- IMAP folders, read/unread and starred flags, manual refresh, configurable polling, and IMAP IDLE inbox notifications when supported. Synchronization can be canceled. Offline flag/move changes are committed locally together with durable queued server actions.
- SQLite with WAL transactions, full-text trigram search, portable backups, restore validation, search-index rebuilding, and `.eml` import/export. Legacy `state.json` data is migrated on first database creation.
- Full or recent-range synchronization. Downloaded mail and attachments remain readable offline; remote disappearance preserves a copy in **Local archive** rather than destroying downloaded mail.
- Threaded/individual lists, a conversation view, HTML/plain/source views, reading-pane placement, multi-selection, bulk actions, drag-to-folder/tag, archive, junk/trash moves, and undo of mail actions.
- Native rich text composition: formatting, headings, lists, quotes, fonts, sizes, colors, alignment, links, undo/redo, rich clipboard, and plain-text sending. Formatted mail includes a plain-text alternative.
- Recipient chips and native address suggestions, To/Cc/Bcc, reply/reply-all, forwarding, forwarding as an attachment, several open drafts, autosave, recovery, signatures, and reusable templates.
- Multiple file attachments, drag and drop, inline images converted to MIME CID attachments, image previews, save/save-all, size limits, and missing-attachment reminders. The total attachment limit is 50 MB; incoming messages are limited to 100 MB.
- Delayed send, an editable Outbox, scheduled sending, explicit failed/uncertain states, stable outgoing Message-IDs, and protection against automatic duplicate SMTP sends. Sent-copy append failures never resend the email.
- Custom colored tags, editable categories, saved searches, local mail rules with match previews, snooze, and follow-up reminders.
- Local contacts, automatic recipient collection, native desktop notifications, quiet hours, private notification previews, unread badges, and optional tray/background operation.
- Light/dark appearances, accent palettes, comfortable/compact density, reading fonts/sizes, preview lengths, avatar visibility, read timing, adjustable panes, remembered windows, native dialogs and menus, and keyboard shortcuts.
- Windows DPAPI protection for credentials, refresh tokens, and OAuth client secrets. TLS certificate checks are enabled. Unencrypted IMAP/SMTP is restricted to literal loopback addresses and localhost.
- HTML allowlist sanitization and a restrictive Content Security Policy. Remote images are blocked by default, with global and trusted-sender options. Active content and remote styles are removed. Links require explicit confirmation before opening in your system browser.

Search examples:

```text
from:alex@example.com subject:proposal
tag:"Client A" is:unread has:attachment
"exact phrase" -subject:newsletter
after:2026-10-01 before:2026-11-01 attachment:pdf
category:Finance folder:INBOX
```

## Google and Microsoft OAuth

Select `oauth2` in the account settings, choose a provider, and supply a registered desktop/public application's client ID. Google may also issue a desktop client secret. **Sign in securely** opens the system browser and uses a random loopback port, state validation, and PKCE. Access and refresh tokens are encrypted for the current Windows user; refresh is automatic.

- **Google:** register a Desktop OAuth client and allow the intended users in the consent configuration. The application requests `https://mail.google.com/` for IMAP/SMTP. [Google desktop OAuth documentation](https://developers.google.com/identity/protocols/oauth2/native-app).
- **Microsoft:** register a public/native desktop application, enable the required delegated IMAP and SMTP permissions, and register the loopback redirect `http://localhost/oauth/callback` (localhost redirect ports are dynamic). Scopes include offline access, IMAP access, and SMTP send. Workplace consent policies and SMTP AUTH availability still apply. [Microsoft IMAP/SMTP OAuth documentation](https://learn.microsoft.com/en-us/exchange/client-developer/legacy-protocols/how-to-authenticate-an-imap-pop-smtp-application-by-using-oauth).

There is no bundled production OAuth application registration. A manually obtained access token can also be saved in the protected credential field, but without a refresh token it expires normally. Provider sign-in and delivery to real external accounts require user credentials and have not been exercised in this workspace.

## Storage and operational behavior

`mail.sqlite` stores messages, drafts, metadata, queued changes, and the search index. `secrets.dat` stores DPAPI-protected credentials. Mail itself is not encrypted at rest by Bentomail; normal Windows account/storage protection applies. Backups include mail and preferences, exclude credentials and pending server actions, and turn scheduled messages back into drafts on restore. Restore refuses to proceed while offline server changes remain pending.

Large HTML bodies/inline images exceeding the embedded preview budget fall back to native plain text; attachments remain available separately. The app uses MyGo's single-instance lock so a second launch focuses the existing window.

Synchronization keeps normal provider-side copies. Local storage does not automatically remove mail from the server. Removing an account retains downloaded mail in Local archive and removes its credentials.

Scheduled sends, rules, snooze, reminders, and IDLE monitoring operate while Bentomail is running. Enable tray operation to keep it running after closing the main window. A queued message can be canceled by opening **Outbox → Edit draft** before delivery begins.

If SMTP disconnects after message DATA, or the app closes during delivery, the draft becomes **uncertain**. It is never automatically retried. Inspect your server Sent folder and recipient state before explicitly editing/retrying it. Exact-once delivery cannot be guaranteed by SMTP.

Remote moves require the server's `MOVE` extension. Unsupported moves stay queued and produce an actionable error; Bentomail does not expunge unrelated messages as a fallback. Tags are always local; atom-compatible tags may be pushed as IMAP keywords during remote message updates, but arbitrary provider label systems are not fully mirrored. Drafts, rules, snooze, follow-ups, and templates are local.

## Logo

The application uses a filled envelope and bento mark with transparent dividers and background, including a variant for small sizes. The earlier vector concepts and review notes are preserved under `resources/logo-concepts/`.

Regenerate the original concept gallery with `node scripts/create_logos.mjs`. Render the application PNG/ICO assets with `go run ./cmd/brand-assets`.

## Build and verification

In **Settings → Appearance**, adjust **Translucency** from 0% (opaque) to a maximum of 65% (frosted). The default is 65%; older saved or restored values above 65% are capped automatically. Changes apply immediately and persist across launches and backups. Windows 11 22H2 and later use native Acrylic blur, with a solid window background on systems without native materials. Translucent panes, dropdowns, and dialogs soften their backdrop with a 32 DIP blur, while button faces remain at least 94% opaque. Floating surfaces retain at least 80% tint opacity so background text cannot compete with their labels; recipient suggestions use a solid surface. Native menus, context menus, and their item highlights blend over a similarly strong blurred background captured when each popup opens; menu text stays opaque. These temporary captures remain in memory until the menu closes, and unavailable capture falls back to solid menu surfaces. Menus follow the app's font and rounded selection styling; confirmation dialogs share the panels' corner radius. Both sidebar states have extra padding and a scrollbar gutter; Settings and Update stay icon-only, stacked when collapsed and side by side when expanded.

Requires the Go version pinned in `go.mod`. Run `./build.ps1` on Windows to test, run MyGo's UI checks, and build the application and installer. `go tool mygo build` builds directly for the current platform. To cross-compile all desktop targets:

```sh
go tool mygo build -platform windows/amd64,windows/arm64,linux/amd64,linux/arm64,darwin/universal
```

Creating macOS disk images and signing the app requires macOS; Windows installers require NSIS, which MyGo downloads automatically on Windows.

GitHub Actions builds all desktop targets on pushes, pull requests, and manual runs, using one matrix with Windows, Linux, and macOS runners. Tests and MyGo's UI checks run on Windows because credential storage tests use DPAPI. Download the `bentomail-Windows`, `bentomail-Linux`, or `bentomail-macOS` workflow artifact for the apps, installers, and required resources. Icons are checked in; regenerate them with `go run ./cmd/brand-assets` when the artwork changes.

After a successful push or manual run on the default branch, the workflow automatically publishes a GitHub Release whose tag and title are both `bentomail-v<version>`, such as `bentomail-v0.2.5`. The next version increments the highest existing release's patch version; `mygo.json` sets the minimum version, so changing it can start a new minor or major version. Legacy `build-0.2.x` releases count during migration, and drafts reserve their versions. The workflow run number is never used. One version is chosen for every platform, the app version, and the signed archive URLs. All platform builds must succeed, and the publishing script checks every update manifest's version, archive URL, and size before publishing the complete draft release. Rerunning failed jobs can finish interrupted drafts; published versions are left intact. Pull requests and other branches only build and upload workflow artifacts.

The sidebar toggle switches between the resizable sidebar and a compact icon rail, and remembers the choice. The account switcher displays initials avatars beside each account name and address. The composer schedule field shows its full date/time placeholder.

**Update app** in the sidebar opens MyGo's native updater to check, download, verify, install, and restart. Background checks run daily. Builds embed the update public key and find signed manifests on `bentomail-v` releases in `Ahdeyyy/bentomail`. CI signs using the `MYGO_UPDATER_PRIVATE_KEY` Actions secret; each target has its own `update-<target>.json` and signed archive, including `update-darwin-universal.json` for both Mac architectures. Keep the signing key backed up outside the repository. Local signed builds need the same private key in `MYGO_UPDATER_PRIVATE_KEY`. Development builds report that updates are unavailable. Installations that use the old `build-` feed, and existing 0.1 installations, need one manual installation of a new release to discover the new feed.

SMTP submission pairs port 587 (and 25) with STARTTLS and port 465 with direct TLS, correcting older saved accounts at connection time and on their next save. Custom ports retain the selected security mode. TLS certificate validation remains required, and failed encryption never falls back to plaintext. Failed Outbox messages can be retried with **Send now** after upgrading; uncertain deliveries still require checking Sent before retrying.

```powershell
$env:CGO_ENABLED = '0'
go test ./...
go tool mygo vet .
go vet ./...
```

Tests cover local SMTP delivery and ambiguous completion, Bcc privacy, Unicode display names, aliases, MIME/inline attachment round trips, IMAP synchronization, offline moves, duplicate prevention, encrypted credential persistence, backup/restore, full-text indexing, sanitization, native editor behavior, and control visibility at minimum window size.

To export native UI screenshots with synthetic test mail:

```powershell
$env:BENTOMAIL_SCREENSHOTS = 'scratch/screenshots'
go test . -run TestRenderWorkspace -count=1
```

The test mailbox is only created in temporary test storage; the installed application starts empty. Race instrumentation requires cgo and a C compiler, which are not available in the current environment. Windows OS notification/tray behavior, provider-specific OAuth consent, and live external mail delivery need validation on the target user's machine.

Martian Grotesk is bundled under the SIL Open Font License in `resources/fonts/OFL.txt`. Dependency versions are pinned in `go.mod` and `go.sum`.
