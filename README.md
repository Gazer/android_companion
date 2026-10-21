# Android Companion

A desktop app that launches an Android emulator **embedded inside the window**, or mirrors a **physical device over USB or Wi-Fi**, with a live **logcat** panel and a **plugin system** for log-driven widgets (line charts, status badges, counters).

Built with **Go + Fyne**. Emulators use their **gRPC bridge** (the same mechanism Android Studio uses to embed the emulator); physical devices are mirrored with an embedded **[scrcpy](https://github.com/Genymobile/scrcpy) server**, decoded with **FFmpeg**.

![screenshot](screenshot.png)

## Features

### Devices

- Picker with the connected devices (USB, Wi-Fi, running emulators) and the AVDs to launch; it refreshes as devices come and go
- Pair a device over Wi-Fi with a pairing code (Android 11+), or `adb connect` to it
- If the device disconnects, the app goes back to the picker and keeps its logs
- Pass `-avd <name>` or `-serial <serial>` to skip the picker

### Screen

- Emulators run embedded; physical devices are mirrored (up to 60 fps, 1280 px), Android 5.0+
- Click, tap and drag on the screen; mouse wheel / trackpad scrolling on devices
- Click the screen to type into it: text, Enter, Backspace, arrows, Tab, Esc (back); Cmd/Ctrl+V pastes
- On-screen back / home / recents buttons
- Stop / Disconnect button, back to the picker; closing the window also kills the emulator (devices are left alone)
- If mirroring isn't possible, the device still gets logs and plugins

### Logcat

- Android Studio look: aligned columns, colored tags and severity levels
- Multi-line messages
- Package picker plus free-text filter: space-separated terms that must all match, `~term` hides lines containing `term`, `"a phrase"` keeps spaces, `\~` for a literal `~` (e.g. `error ~Choreographer ~gc`)
- The last package and filter are restored on the next start
- Auto-scroll that stops when you scroll up and resumes with a jump-to-bottom button
- Horizontal scrolling for long lines
- The log keeps up even on chatty apps: only visible rows are rendered

### Plugins

Widgets driven by log lines, in a collapsible right column. Add them from the **Add** menu, edit and remove them; configs persist across restarts.

**Line chart** — plot a value extracted from matching lines in real time:

```
pattern: frame took {%d}ms
```

`{%d}` captures an integer, `{%f}` a decimal, `{%s}` a word.

**Status badge** — a colored badge that reflects the last matching rule:

```
rule 1: "player started"  -> Playing (green)
rule 2: "player paused"   -> Paused  (amber)
```

Captured values can be shown in the label:

```
pattern: buffering at {%d}%
label:   Buffering $0%     -> badge shows "Buffering 42%"
```

**Counter** — counts the log lines that contain a pattern, with a Reset button:

```
pattern: FATAL EXCEPTION   -> 3
```

The whole line is searched (tag included), case-sensitive; `{%d}` `{%f}` `{%s}` match any number / word. The count starts at 0 on every run.

## Install

### 1. Requirements

| | macOS | Linux (Debian / Ubuntu) |
|---|---|---|
| Go 1.25+ | `brew install go` | [go.dev/dl](https://go.dev/dl) |
| C compiler (cgo) | `xcode-select --install` | `sudo apt install build-essential` |
| OpenGL / X11 (Fyne) | — | `sudo apt install libgl1-mesa-dev xorg-dev` |
| FFmpeg + pkg-config (device mirroring) | `brew install ffmpeg pkg-config` | `sudo apt install libavcodec-dev libavutil-dev libswscale-dev pkg-config` |
| `adb` | Android Studio's SDK, or `brew install --cask android-platform-tools` | Android Studio's SDK, or `sudo apt install adb` |

- **adb** is the only Android tool that's required. It's taken from the Android SDK (`ANDROID_HOME` / `ANDROID_SDK_ROOT`, or the default `~/Library/Android/sdk` / `~/Android/Sdk`), or from `PATH`.
- **Emulators** (optional) need the Android Emulator SDK package and at least one AVD (create them in Android Studio's Device Manager).
- **FFmpeg** is linked dynamically (tested with FFmpeg 8.1), so it must also be installed where the app runs. Without it, build with `-tags nomirror`: devices still get logs and plugins, without their screen.

### 2. Build

```sh
git clone https://github.com/Gazer/android_companion.git
cd android_companion
make build                                          # or: go build -o android-companion .
go build -tags nomirror -o android-companion .      # alternative: without FFmpeg
```

### 3. Prepare your device (physical devices only)

- Enable **Developer options** and **USB debugging**, plug the device in and accept the debugging prompt.
- **Wi-Fi** (Android 11+): turn on **Wireless debugging**, then use **Pair device over Wi-Fi…** in the app's picker.
- Some Xiaomi devices also need **USB debugging (Security settings)** for input to work.
- **Linux**: your user must be in the `plugdev` group, with udev rules for the device (`sudo apt install android-sdk-platform-tools-common`).

## Run

```sh
./android-companion                         # in-app picker
./android-companion -avd Pixel_8            # launch an AVD, skip picker
./android-companion -avd Pixel_8 -wipe      # cold boot with data wipe
./android-companion -serial 11041FDD4002MQ  # open a connected device (see `adb devices`)
```

Run tests:

```sh
go test ./...
```

## Project layout

- `internal/adb` — device list, device info, Wi-Fi pairing
- `internal/scrcpy` — embedded scrcpy server (v4.0): start/stop, video stream, input messages
- `internal/video` — H.264 decoding with FFmpeg (cgo) and frame buffering
- `internal/avd` — SDK discovery, AVD list, emulator process
- `internal/emulator` — gRPC client for the emulator controller
- `internal/logcat` — `adb logcat` parser and in-memory store
- `internal/plugin` — plugin interface, registry, pattern compiler, persistence (`chart`, `status`, `counter`)
- `internal/ui` — the widgets
- `internal/app` — wiring and lifecycle

## Regenerating protobuf bindings

```sh
make proto
```

Needs `protoc`, `protoc-gen-go` and `protoc-gen-go-grpc`. The proto file is copied from `$ANDROID_HOME/emulator/lib/emulator_controller.proto`.

## Cross-compilation

Fyne uses cgo, so native builds per OS are simplest:

```sh
# On Linux
GOOS=linux go build -o android-companion-linux .

# On macOS
GOOS=darwin go build -o android-companion-darwin .
```

## Third-party

`internal/scrcpy/scrcpy-server-v4.0` is the server of [scrcpy](https://github.com/Genymobile/scrcpy) v4.0 by Genymobile and Romain Vimont, distributed under the Apache License 2.0 (`internal/scrcpy/LICENSE`).

---

Made with ❤️ by Ricardo Markiewicz // [@gazeria](https://x.com/gazeria)
