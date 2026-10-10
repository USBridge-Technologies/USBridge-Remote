[![Tests](https://github.com/fyne-io/glfw-js/actions/workflows/tests.yml/badge.svg)](https://github.com/fyne-io/glfw-js/actions/workflows/tests.yml)
[![Static Analysis](https://github.com/fyne-io/glfw-js/actions/workflows/analysis.yml/badge.svg)](https://github.com/fyne-io/glfw-js/actions/workflows/analysis.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/fyne-io/glfw-js.svg)](https://pkg.go.dev/github.com/fyne-io/glfw-js)

# glfw-js

Package glfw experimentally provides a glfw-like API
with desktop (via glfw) and browser (via HTML5 canvas) backends.

It is used for creating a GL context and receiving events.

**Note:** This package is currently in development. The API is incomplete and may change.

## Installation

```sh
go get github.com/fyne-io/glfw-js
```

## Directories

| Path                                                                | Synopsis                                                           |
|---------------------------------------------------------------------|--------------------------------------------------------------------|
| [test/events](https://pkg.go.dev/github.com/goxjs/glfw/test/events) | events hooks every available callback and outputs their arguments. |

## License

-	[MIT License](LICENSE)
