//go:build !(js && wasm)

package controller

// isWebBuild -- see platform_web.go's doc comment.
const isWebBuild = false
