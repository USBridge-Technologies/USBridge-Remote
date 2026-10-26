//go:build js && wasm

package gui

import "context"

func (mw *MainWindow) waitForSharedNode(context.Context, uint64, string) error { return nil }

func (mw *MainWindow) waitForHostConsent(context.Context, uint64) error { return nil }
