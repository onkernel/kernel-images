Kernel Computer Operator API. To use on PORT=9999

---

# Build
Using bun builder
```bash
bun build:linux # bin : dist/kernel-operator-api
```

---

# Checklist

`[✅ : Works , 〰️ : Yet to be test , ❌ : Doesn't work]`

# Checklist

## /bus
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/bus/publish | ✅ | 〰️ | 〰️ | N/A
/bus/subscribe | ✅ | 〰️ | 〰️ | N/A

## /clipboard
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/clipboard | ✅ | 〰️ | 〰️ | N/A
/clipboard/stream | 〰️ | 〰️ | 〰️ | N/A

## /computer
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/computer/click_mouse | 〰️ | 〰️ | 〰️ | N/A
/computer/move_mouse | 〰️ | 〰️ | 〰️ | N/A

## /fs
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/fs/create_directory | ✅ | 〰️ | 〰️ | N/A
/fs/delete_directory | ✅ | 〰️ | 〰️ | N/A
/fs/delete_file | ✅ | 〰️ | 〰️ | N/A
/fs/download | ✅ | 〰️ | 〰️ | N/A
/fs/file_info | ✅ | 〰️ | 〰️ | N/A
/fs/list_files | ✅ | 〰️ | 〰️ | N/A
/fs/move | ✅ | 〰️ | 〰️ | N/A
/fs/read_file | ✅ | 〰️ | 〰️ | N/A
/fs/set_file_permissions | ✅ | 〰️ | 〰️ | N/A
/fs/tail/stream | 〰️ | 〰️ | 〰️ | N/A
/fs/upload | ✅ | 〰️ | 〰️ | N/A
/fs/watch | 〰️ | 〰️ | 〰️ | N/A
/fs/watch/{watch_id} | 〰️ | 〰️ | 〰️ | N/A
/fs/watch/{watch_id}/events | 〰️ | 〰️ | 〰️ | N/A
/fs/write_file | ✅ | 〰️ | 〰️ | N/A

## /health
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/health | ✅ | 〰️ | 〰️ | N/A

## /input/desktop
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/input/combo/activate_and_type | 〰️ | 〰️ | 〰️ | N/A
/input/combo/activate_and_keys | 〰️ | 〰️ | 〰️ | N/A
/input/combo/window/center | 〰️ | 〰️ | 〰️ | N/A
/input/combo/window/snap | 〰️ | 〰️ | 〰️ | N/A
/input/desktop/count | 〰️ | 〰️ | 〰️ | N/A
/input/desktop/current | 〰️ | 〰️ | 〰️ | N/A
/input/desktop/viewport | 〰️ | 〰️ | 〰️ | N/A
/input/desktop/window_desktop | 〰️ | 〰️ | 〰️ | N/A
/input/display/geometry | 〰️ | 〰️ | 〰️ | N/A
/input/keyboard/key | 〰️ | 〰️ | 〰️ | N/A
/input/keyboard/key_down | 〰️ | 〰️ | 〰️ | N/A
/input/keyboard/key_up | 〰️ | 〰️ | 〰️ | N/A
/input/keyboard/type | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/click | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/down | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/location | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/move | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/move_relative | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/scroll | 〰️ | 〰️ | 〰️ | N/A
/input/mouse/up | 〰️ | 〰️ | 〰️ | N/A
/input/system/exec | 〰️ | 〰️ | 〰️ | N/A
/input/system/sleep | 〰️ | 〰️ | 〰️ | N/A
/input/window/activate | 〰️ | 〰️ | 〰️ | N/A
/input/window/active | 〰️ | 〰️ | 〰️ | N/A
/input/window/close | 〰️ | 〰️ | 〰️ | N/A
/input/window/focus | 〰️ | 〰️ | 〰️ | N/A
/input/window/focused | 〰️ | 〰️ | 〰️ | N/A
/input/window/geometry | 〰️ | 〰️ | 〰️ | N/A
/input/window/kill | 〰️ | 〰️ | 〰️ | N/A
/input/window/map | 〰️ | 〰️ | 〰️ | N/A
/input/window/minimize | 〰️ | 〰️ | 〰️ | N/A
/input/window/move_resize | 〰️ | 〰️ | 〰️ | N/A
/input/window/name | 〰️ | 〰️ | 〰️ | N/A
/input/window/pid | 〰️ | 〰️ | 〰️ | N/A
/input/window/raise | 〰️ | 〰️ | 〰️ | N/A
/input/window/unmap | 〰️ | 〰️ | 〰️ | N/A

## /logs
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/logs/stream | 〰️ | 〰️ | 〰️ | N/A

## /macros
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/macros/create | 〰️ | 〰️ | 〰️ | N/A
/macros/list | 〰️ | 〰️ | 〰️ | N/A
/macros/run | 〰️ | 〰️ | 〰️ | N/A
/macros/{macro_id} | 〰️ | 〰️ | 〰️ | N/A

## /metrics
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/metrics/snapshot | 〰️ | 〰️ | 〰️ | N/A
/metrics/stream | 〰️ | 〰️ | 〰️ | N/A

## /network/forward
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/network/forward | 〰️ | 〰️ | 〰️ | N/A
/network/forward/{forward_id} | 〰️ | 〰️ | 〰️ | N/A
/network/har/stream | 〰️ | 〰️ | 〰️ | N/A
/network/intercept/rules | 〰️ | 〰️ | 〰️ | N/A
/network/intercept/rules/{rule_set_id} | 〰️ | 〰️ | 〰️ | N/A
/network/proxy/socks5/start | 〰️ | 〰️ | 〰️ | N/A
/network/proxy/socks5/stop | 〰️ | 〰️ | 〰️ | N/A

## /os
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/os/locale | 〰️ | 〰️ | 〰️ | N/A

## /pipe
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/pipe/recv/stream | 〰️ | 〰️ | 〰️ | N/A
/pipe/send | 〰️ | 〰️ | 〰️ | N/A

## /process
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/process/exec | 〰️ | 〰️ | 〰️ | N/A
/process/spawn | 〰️ | 〰️ | 〰️ | N/A
/process/{process_id}/kill | 〰️ | 〰️ | 〰️ | N/A
/process/{process_id}/status | 〰️ | 〰️ | 〰️ | N/A
/process/{process_id}/stdin | 〰️ | 〰️ | 〰️ | N/A
/process/{process_id}/stdout/stream | 〰️ | 〰️ | 〰️ | N/A

## /recording
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/recording/delete | 〰️ | 〰️ | 〰️ | N/A
/recording/download | 〰️ | 〰️ | 〰️ | N/A
/recording/list | 〰️ | 〰️ | 〰️ | N/A
/recording/start | 〰️ | 〰️ | 〰️ | N/A
/recording/stop | 〰️ | 〰️ | 〰️ | N/A

## /screenshot
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/screenshot/capture | 〰️ | 〰️ | 〰️ | N/A
/screenshot/{screenshot_id} | 〰️ | 〰️ | 〰️ | N/A

## /scripts
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/scripts/delete | 〰️ | 〰️ | 〰️ | N/A
/scripts/list | 〰️ | 〰️ | 〰️ | N/A
/scripts/run | 〰️ | 〰️ | 〰️ | N/A
/scripts/run/{run_id}/logs/stream | 〰️ | 〰️ | 〰️ | N/A
/scripts/upload | 〰️ | 〰️ | 〰️ | N/A

## /stream
Endpoint/service | API Build | Kernel:Docker | Kernel:Unikraft | Notes
--- | --- | --- | --- | ---
/stream/start | 〰️ | 〰️ | 〰️ | N/A
/stream/stop | 〰️ | 〰️ | 〰️ | N/A
/stream/{stream_id}/metrics/stream | 〰️ | 〰️ | 〰️ | N/A

---

# Tests

```bash
bun test.js browser --watch
bun test.js bus --watch
bun test.js clipboard --watch
bun test.js fs --watch
bun test.js fs-nodelete --watch
bun test.js health --watch
bun test.js input --watch
bun test.js logs --watch
bun test.js macros --watch
bun test.js metrics --watch
bun test.js network --watch
bun test.js os --watch
bun test.js pipe --watch
bun test.js process --watch
bun test.js recording --watch
bun test.js recording-nodelete --watch
bun test.js screenshot --watch
bun test.js scripts --watch
bun test.js scripts-nodelete --watch
bun test.js stream --watch
```

---

# Notes

#### Add to/ensure exists in Dockerfile

```bash
wayland tools , wl-paste , xclip # should be part of wayland but missing in my dev vm
```