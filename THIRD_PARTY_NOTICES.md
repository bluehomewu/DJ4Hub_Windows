# Third-Party Notices

DJ 4G Hub for Windows is a port of [WongLoki/DJ4Hub](https://github.com/WongLoki/DJ4Hub). DJ 4G Hub contains code derived from the upstream VoHive project and work inspired by ZenGeekLabs/DJOneHub. It retains the license and required notice provided in the repository root [`LICENSE`](LICENSE):

```text
Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
```

## Release Runtime

The Windows release is a single statically linked `dj4ghub.exe`. It bundles no native libraries; the upstream macOS package's libusb is not used. Go modules compiled into the executable, such as `go.bug.st/serial` (BSD-3-Clause), `golang.org/x/sys` (BSD-3-Clause) and `modernc.org/sqlite` (BSD-3-Clause), retain their own licenses.

## Vendored Source Dependencies

The source repository includes vendored dependencies under `third_party/` so the versions used by DJ 4G Hub remain reproducible. Their original copyright notices and license texts are retained in the corresponding directories.

| Component | License file |
| --- | --- |
| euicc-go | `third_party/euicc-go/LICENSE` |
| uicc-go | `third_party/uicc-go/LICENSE` |
| quectel-qmi-go | `third_party/quectel-qmi-go/LICENSE` |
| strftime | `third_party/strftime/LICENSE` |
| pkg/errors | `third_party/pkg-errors/LICENSE` |
| golang.org/x/text | `third_party/x-text/LICENSE` |
| multierr | `third_party/multierr/LICENSE.txt` |

Dependencies fetched through Go modules retain their own licenses and copyright notices. This file is informational and does not replace any component's full license text.
