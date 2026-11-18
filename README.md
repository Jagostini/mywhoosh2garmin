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
| Windows | `mywhoosh2garmin-windows-amd64.exe` |
| Linux | `mywhoosh2garmin-linux-amd64` |
| macOS (Apple Silicon) | `MyWhoosh2Garmin-macos-arm64.dmg` |
| Android | `MyWhoosh2Garmin-android.apk` |

On desktop, no installation is needed — just download and run.

### Android note

1. Download `MyWhoosh2Garmin-android.apk` from the release page on your phone.
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

### 1. Enter MyWhoosh credentials

Enter your **MyWhoosh email** and **password**. These are used to log in to the MyWhoosh API and fetch your activity list.

After the first login, the session token is cached locally (`~/.mywhoosh2garmin/`) so you won't need to enter your password again unless the token expires.

### 2. Enter Garmin credentials

Enter your **Garmin Connect email** and **password**. These are only sent directly to Garmin's SSO servers — never stored or sent anywhere else.

After the first login, a session token is cached locally (`~/.mywhoosh2garmin/`) and reused for up to a year. You won't need to enter your password again unless the token expires.

### 3. Garmin MFA (if enabled on your account)

If Garmin sends you a 6-digit verification code by email, enter it in the **MFA code** field and press **Enter Code** (or **Check Garmin Login** again). The resulting session token is cached, so you only need to do this on the first login or after the token expires.

### 4. One-tap Sync

Tap the big **🚀 SYNCHRONISER** button at the top of the window. It does steps 5 and 6 in one go: it fetches the last 10 days of activities, then uploads every activity that has not been synced yet. This is the easiest way to use the app, especially on a phone.

### 5. Fetch Activities

Click **📋 Fetch Activities (last 10 days)** and the app will:

1. Log in to your MyWhoosh account (or resume a cached session)
2. Fetch your activities from the last 10 days
3. Display them in a list showing date, title, distance, duration, power, and heart rate — with an upload button next to each one

### 6. Upload to Garmin

You can either:

- Click **⬆ Upload** next to individual activities to upload them one by one
- Click **⬆ Upload All to Garmin** to upload all unsynced activities at once

For each activity, the app will:

1. Download the FIT file from MyWhoosh
2. Fix averages, strip temperature, spoof device identity
3. Upload to Garmin Connect
4. Mark the activity as synced so it won't be uploaded again

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
