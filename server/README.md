# Kernel Images Server

A REST API server to start, stop, and download screen recordings.

## 🛠️ Prerequisites

### Required Software

- **Go 1.24.3+** - Programming language runtime
- **ffmpeg** - Video recording engine
  - macOS: `brew install ffmpeg`
  - Linux: `sudo apt install ffmpeg` or `sudo yum install ffmpeg`
- **pnpm** - For OpenAPI code generation
  - `npm install -g pnpm`

### System Requirements

- **macOS**: Uses AVFoundation for screen capture
- **Linux**: Uses X11 for screen capture
- **Windows**: Not currently supported

## 🚀 Quick Start

### Running the Server

```bash
make dev
```

The server will start on port 10001 by default and log its configuration.

#### Example use

```bash
# 1. Start a new recording
curl http://localhost:10001/recording/start -d {}

# (recording in progress)

# 2. Stop recording
curl http://localhost:10001/recording/stop -d {}

# 3. Download the recorded file
curl http://localhost:10001/recording/download --output recording.mp4
```

#### Additional routes

```
# Input features ---
# Mouse

# | POST /input/mouse/move — Move mouse to absolute coordinates
curl -X POST -H "Content-Type: application/json" \
  --data '{"x": 500, "y": 500}' \
  http://localhost:10001/input/mouse/move
# Response: {"ok":true}

# | POST /input/mouse/move_relative — Move mouse relative to current position
curl -X POST -H "Content-Type: application/json" \
  --data '{"dx": 50, "dy": -25}' \
  http://localhost:10001/input/mouse/move_relative
# Response: {"ok":true}

# | POST /input/mouse/click — Click mouse button
curl -X POST -H "Content-Type: application/json" \
  --data '{"button":"left","count":2}' \
  http://localhost:10001/input/mouse/click
# Response: {"ok":true}

# | POST /input/mouse/down — Press mouse button down
curl -X POST -H "Content-Type: application/json" \
  --data '{"button":"left"}' \
  http://localhost:10001/input/mouse/down
# Response: {"ok":true}

# | POST /input/mouse/up — Release mouse button
curl -X POST -H "Content-Type: application/json" \
  --data '{"button":"left"}' \
  http://localhost:10001/input/mouse/up
# Response: {"ok":true}

# | POST /input/mouse/scroll — Scroll mouse wheel
curl -X POST -H "Content-Type: application/json" \
  --data '{"dx":0,"dy":-120}' \
  http://localhost:10001/input/mouse/scroll
# Response: {"ok":true}

# | GET /input/mouse/location — Get current mouse location
curl http://localhost:10001/input/mouse/location
# Response: {"x":500,"y":500,"screen":0,"window":"60817493"}


# Keyboard

# | POST /input/keyboard/type — Type text
curl -X POST -H "Content-Type: application/json" \
  --data '{"text":"Hello, World!","wpm":300,"enter":true}' \
  http://localhost:10001/input/keyboard/type
# Response: {"ok":true}

# | POST /input/keyboard/key — Send key sequence
curl -X POST -H "Content-Type: application/json" \
  --data '{"keys":["ctrl","a"]}' \
  http://localhost:10001/input/keyboard/key
# Response: {"ok":true}

# | POST /input/keyboard/key_down — Press and hold a key
curl -X POST -H "Content-Type: application/json" \
  --data '{"key":"ctrl"}' \
  http://localhost:10001/input/keyboard/key_down
# Response: {"ok":true}

# | POST /input/keyboard/key_up — Release a key
curl -X POST -H "Content-Type: application/json" \
  --data '{"key":"ctrl"}' \
  http://localhost:10001/input/keyboard/key_up
# Response: {"ok":true}


# Window

# | POST /input/window/activate — Activate a window by match
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"New Tab","only_visible":true}}' \
  http://localhost:10001/input/window/activate
# Response: {"activated":true,"wid":"60817493"}

# | POST /input/window/focus — Focus a window by match
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"class":"Google-chrome"}}' \
  http://localhost:10001/input/window/focus
# Response: {"focused":true,"wid":"60817493"}

# | POST /input/window/move_resize — Move/resize a window by match
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"Chrome"},"x":0,"y":0,"width":1280,"height":720}' \
  http://localhost:10001/input/window/move_resize
# Response: {"ok":true}

# | POST /input/window/raise — Raise window to top
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"pid":12345}}' \
  http://localhost:10001/input/window/raise
# Response: {"ok":true}

# | POST /input/window/minimize — Minimize window
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"New Tab"}}' \
  http://localhost:10001/input/window/minimize
# Response: {"ok":true}

# | POST /input/window/map — Show window
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"New Tab"}}' \
  http://localhost:10001/input/window/map
# Response: {"ok":true}

# | POST /input/window/unmap — Hide window
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"New Tab"}}' \
  http://localhost:10001/input/window/unmap
# Response: {"ok":true}

# | POST /input/window/close — Close window by match
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"New Tab"}}' \
  http://localhost:10001/input/window/close
# Response: {"ok":true,"wid":"60817493","windowIds":["60817493"]}

# | POST /input/window/kill — Force-kill window by match
curl -X POST -H "Content-Type: application/json" \
  --data '{"match":{"title_contains":"Unresponsive App"}}' \
  http://localhost:10001/input/window/kill
# Response: {"ok":true}

# | GET /input/window/active — Get active window
curl http://localhost:10001/input/window/active
# Response: {"wid":"60817493"}

# | GET /input/window/focused — Get focused window
curl http://localhost:10001/input/window/focused
# Response: {"wid":"60817493"}

# | POST /input/window/name — Get window name
curl -X POST -H "Content-Type: application/json" \
  --data '{"wid":"60817493"}' \
  http://localhost:10001/input/window/name
# Response: {"wid":"60817493","name":"New Tab - Google Chrome"}

# | POST /input/window/pid — Get window PID
curl -X POST -H "Content-Type: application/json" \
  --data '{"wid":"60817493"}' \
  http://localhost:10001/input/window/pid
# Response: {"wid":"60817493","pid":42420}

# | POST /input/window/geometry — Get window geometry
curl -X POST -H "Content-Type: application/json" \
  --data '{"wid":"60817493"}' \
  http://localhost:10001/input/window/geometry
# Response: {"wid":"60817493","x":0,"y":0,"width":1280,"height":720,"screen":0}


# Display

# | GET /input/display/geometry — Get display geometry
curl http://localhost:10001/input/display/geometry
# Response: {"width":1536,"height":776}
```

### ⚙️ Configuration

Configure the server using environment variables:

| Variable       | Default   | Description                                 |
| -------------- | --------- | ------------------------------------------- |
| `PORT`         | `10001`   | HTTP server port                            |
| `FRAME_RATE`   | `10`      | Default recording framerate (fps)           |
| `DISPLAY_NUM`  | `1`       | Display/screen number to capture            |
| `MAX_SIZE_MB`  | `500`     | Default maximum file size (MB)              |
| `OUTPUT_DIR`   | `.`       | Directory to save recordings                |
| `FFMPEG_PATH`  | `ffmpeg`  | Path to the ffmpeg binary                   |

#### Example Configuration

```bash
export PORT=8080
export FRAME_RATE=30
export MAX_SIZE_MB=1000
export OUTPUT_DIR=/tmp/recordings
./bin/api
```

### API Documentation

- **YAML Spec**: `GET /spec.yaml`
- **JSON Spec**: `GET /spec.json`

## 🔧 Development

### Code Generation

The server uses OpenAPI code generation. After modifying `openapi.yaml`:

```bash
make oapi-generate
```

## 🧪 Testing

### Running Tests

```bash
make test
```