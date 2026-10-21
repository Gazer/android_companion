# Android Companion

A desktop app that launches an Android emulator **embedded inside the window** with a live, scrollable and filterable **logcat** panel on the right.

Built with **Go + Fyne** (native cross-platform UI) and the emulator's **gRPC bridge** (the same mechanism Android Studio uses to embed the emulator).

## Features

- AVD picker on launch (or pass `-avd <name>` to skip it)
- Resizable split view: emulator on the left, logcat on the right (drag the divider)
- Emulator runs headless; its screen is streamed via gRPC `streamScreenshot` (RGB888) into a Fyne canvas
- Click/tap and drag directly on the emulator view (forwarded via gRPC `sendMouse`)
- Live logcat with:
  - virtualized list (only renders visible rows)
  - filter box (matches raw line, tag, or message; case-insensitive)
  - auto-scroll to bottom
  - bounded ring buffer (100k entries) to cap memory
- Closing the window kills the emulator

## Requirements

- Go 1.22+
- Android SDK with `emulator` and `adb` (`ANDROID_HOME` respected)
- **Linux**: `sudo apt install libgl1-mesa-dev xorg-dev`
- **macOS**: no extra deps (Fyne uses system frameworks)
- For regenerating protobufs: `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`

## Build & Run

```sh
make build
./android-companion              # in-app AVD picker
./android-companion -avd Pixel_8 # skip picker, launch directly
./android-companion -avd Pixel_8 -wipe  # cold boot with data wipe
```

## Architecture

```
┌─────────────────────────────┬──────────────────┐
│                             │  Filter: [____]  │
│   Emulator screen           ├──────────────────┤
│   (Fyne canvas.Raster       │  I/ActivityTask..│
│    from gRPC RGB888 stream) │  W/PackageMana.. │
│                             │  D/MyApp: hello  │
│   mouse/drag forwarded      │  ...             │
│   via gRPC                  │  (virtualized    │
│                             │   widget.List)   │
│        ◄── draggable split ──┤                  │
└─────────────────────────────┴──────────────────┘
```

- `internal/avd` — discover SDK, list AVDs, launch/kill emulator
- `internal/emulator` — gRPC client for `EmulatorController` (generated from `emulator_controller.proto`)
- `internal/logcat` — `adb logcat` streaming parser
- `internal/ui` — Fyne widgets: picker, emulator canvas, logcat panel, main window with `HSplit`
- `internal/app` — lifecycle orchestration, gRPC/logcat goroutines, thread-safe UI updates via `fyne.Do`

## Regenerating protobuf bindings

```sh
make proto
```

The proto file is copied from `$ANDROID_HOME/emulator/lib/emulator_controller.proto`.

## Cross-compilation

Fyne uses cgo (OpenGL/system UI), so native builds per OS are simplest:

```sh
# On Linux
GOOS=linux go build -o android-companion-linux .

# On macOS
GOOS=darwin go build -o android-companion-darwin .
```
