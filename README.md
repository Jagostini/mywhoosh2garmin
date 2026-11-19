# MyWhoosh2Garmin

A single-executable GUI app (desktop and Android) that syncs your [MyWhoosh](https://www.mywhoosh.com/) indoor cycling activities to [Garmin Connect](https://connect.garmin.com/) — with full training effect, VO2max, and performance stats support.

No need to run this on the same PC as MyWhoosh — the app downloads activities directly from your MyWhoosh account.


## Why?

MyWhoosh exports FIT files, but they have issues that prevent Garmin from fully processing them:

- **Missing averages** — average power, heart rate, and cadence are not set in the session data
- **Bogus temperature** — every record contains a fake temperature reading
- **Unknown device** — Garmin ignores training effect and VO2max from unknown manufacturers

MyWhoosh2Garmin fixes all of this automatically:

| Problem | Fix |
|---|---|
| Missing avg power / HR / cadence | Calculated from ride records |
| Fake temperature data | Stripped from all records |
| MyWhoosh device identity | Spoofed to Garmin Fenix 6S Pro |

The result: your indoor rides show up on Garmin Connect just like a native Garmin recording, complete with **Training Effect**, **VO2max updates**, **Training Load**, and **Training Status**.

## Download

Grab the latest release for your platform from the [**Releases**](../../releases) page:

| Platform | File |
|---|---|
| Windows | `MyWhoosh2Garmin-<version>-windows-amd64.exe` |
| Linux | `MyWhoosh2Garmin-<version>-linux-amd64` |
| macOS (Apple Silicon) | `MyWhoosh2Garmin-<version>-macos-arm64.dmg` |
| Android | `MyWhoosh2Garmin-<version>.apk` |

On desktop, no installation is needed — just download and run.

### Android note

1. Download `MyWhoosh2Garmin-<version>.apk` from the release page on your phone.
2. Open it and allow installation from unknown sources when Android asks (the APK is signed with a debug key, not published on the Play Store).
3. Launch **MyWhoosh2Garmin**, enter your credentials once, then tap the big **🚀 SYNCHRONISER** button.

On Android, settings and tokens are stored in the app's private storage and are removed if you uninstall the app.

### macOS note

Because the app is not notarized by Apple, macOS may show **"damaged and can't be opened"** the first time. To clear this, run once in Terminal:

```bash
xattr -cr /path-to/MyWhoosh2Garmin.app
```

Or right-click the `.app` inside the DMG and choose **Open**, then click **Open** in the prompt.

## How to Use

The main screen is built around one big round button. Account details live in the settings screen (gear icon, top right), so day to day you only tap **Synchroniser**.

### 1. First launch: settings

On first launch the app opens the settings screen directly:

- Enter your **MyWhoosh** email and password.
- Enter your **Garmin Connect** email and password. If Garmin sends you a 6-digit code by email (MFA), enter it in the MFA field.
- Tap **Enregistrer et tester**. Both connections are checked, and you land on the main screen.

Passwords are only sent to MyWhoosh and Garmin's own servers. After the first login, session tokens are cached locally and reused (Garmin sessions last about a year), so you won't be asked again unless they expire.

### 2. Synchronise

Tap the big button. The app fetches your last 10 days of MyWhoosh activities and uploads the ones that are not on Garmin yet. The button shows what is going on:

| State | Look | Meaning |
|---|---|---|
| Ready | Dark disc, **Synchroniser**, a badge such as "2 nouvelles" | New activities are waiting (checked automatically at startup) |
| Running | Progress ring, e.g. **2/3** | Downloading, fixing and uploading |
| Done | Green disc, **À jour** | Everything is on Garmin |
| Action required | Red disc, **Réessayer**, plus a shortcut button | A login or MFA code is needed; the shortcut opens the settings |

Under the button you see the date of the last sync and a short summary, for example "1 envoyée, 1 déjà sur Garmin".

For each activity, the app:

1. Downloads the FIT file from MyWhoosh
2. Fixes averages, strips temperature, spoofs device identity
3. Uploads it to Garmin Connect
4. Remembers it, so it is never uploaded twice

### 3. Journal and activities

The **Afficher le journal** button reveals the list of recent activities (synced or waiting) and a technical log. On wide screens (tablet, desktop) the app switches to two columns and always shows this panel on the right.

## Building from Source

### Prerequisites

- Go 1.24+
- GCC (for Fyne/CGO)
- Windows cross-compile from Linux: `mingw-w64`, `libgl-dev`, `xorg-dev`, `libxxf86vm-dev`
- Android: Android NDK and the [Fyne CLI](https://docs.fyne.io/started/packaging) (`go install fyne.io/tools/cmd/fyne@latest`)

### Build

```bash
# Linux
go build -o mywhoosh2garmin .

# Both platforms (requires mingw-w64)
./build.sh
# Output in dist/
```

### Android APK

```bash
export ANDROID_NDK_HOME=/path/to/android/ndk
fyne package --target android \
  --app-id com.pgee70.mywhoosh2garmin \
  --name MyWhoosh2Garmin \
  --icon images/icon.png
```

The GitHub Actions workflow (`.github/workflows/release.yml`) builds the APK automatically:

- on a `v*` tag push — also publishes the APK on the GitHub release;
- on a push to any `android-*` branch — build only, for testing (download the APK from the run's artifacts);
- manually, via **Actions → Build & Release → Run workflow**.

### Run tests

```bash
go test ./...
```

## How It Works

```
  ┌─────────────────────┐
  │  MyWhoosh Web API   │
  │  Login + List       │
  │  Download FIT file  │
  └─────────┬───────────┘
            │
            ▼
  ┌─────────────────────┐
  │  Decode FIT (V2)    │
  │  Fix session avgs   │
  │  Strip temperature  │
  │  Spoof → Fenix 6S   │
  │  Encode FIT (V2)    │
  └─────────┬───────────┘
            │
            ▼
  ┌─────────────────────┐
  │  Garmin SSO Login   │
  │  OAuth1 → OAuth2    │
  │  Upload FIT file    │
  └─────────────────────┘
            │
            ▼
     Garmin Connect
  (Training Effect ✓)
  (VO2max ✓)
  (Training Load ✓)
```

## Where data is stored

| Platform | Location |
|---|---|
| Desktop | `~/.mywhoosh2garmin/` (config, session tokens, synced-activity list) |
| Android | The app's private storage (not accessible to other apps) |

## Credits

- [garth](https://github.com/matin/garth) by matin — Garmin SSO authentication reference
- [muktihari/fit](https://github.com/muktihari/fit) — FIT SDK for Go
- [Fyne](https://fyne.io/) — cross-platform GUI toolkit

## License

GPLv3 — see [LICENSE](LICENSE).
