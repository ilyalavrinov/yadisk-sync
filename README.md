[![Build Status](https://travis-ci.com/admirallarimda/yadisk-sync.svg?branch=master)](https://travis-ci.com/admirallarimda/yadisk-sync)
# yadisk-sync
Tool for explicit synching files with Yandex.Disk

This tool works via WebDAV protocol, thus you could try it with any other WebDAV-compatible file server.

## How to use
Simply run it like this: _./yadisk-sync --from [from] --to [to] --user admirallarimda_

If [from] is a directory, all subsequent directories will be stored on a server as well, preserving directory structure. File masking is not yet supported (i.e. you can't write something like --from *.jpg)

[to] points to the folder on a server where all data should be stored ([to] directory might be missing, the tool will create it automatically)

## Building
go 1.11 is preferable (due to modules support). Though you should be able to compile with lower versions of go toolset.

Due to [gowebdav memleak](https://github.com/studio-b12/gowebdav/issues/24) not the latest version of the library is used. Once the bug is fixed, I'll update the module.
