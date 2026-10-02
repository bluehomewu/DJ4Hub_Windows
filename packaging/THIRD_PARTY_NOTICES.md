# Third-Party Notices

DJ 4G Hub for Windows is a single statically linked executable and bundles no native libraries. Go modules compiled into it, such as `go.bug.st/serial`, `golang.org/x/sys` and `modernc.org/sqlite`, retain their own licenses.

DJ 4G Hub is distributed under the terms in `LICENSE`, including its required upstream notice:

```text
Required Notice: Copyright iniwex5 (https://github.com/iniwex5/vohive)
```

Quectel USB drivers are not included. Obtain and install them from Quectel or your module supplier under their terms.

## Optional experimental module audio

The audio control code can use locally supplied QDC507 kernel modules and the
MaVo PCM bridge. These
third-party binaries and Android Platform Tools are not included in this
package. See `docs/QDC507_AUDIO_RESEARCH.md` for provenance, hashes and tested
scope.
