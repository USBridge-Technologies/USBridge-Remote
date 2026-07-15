# Virtual keyboard for fullscreen mode

## Overview of changes

The old, broken key capture in fullscreen mode has been replaced with a virtual keyboard that runs in a separate window.

## New features

### 1. Visible button in fullscreen mode
- A ⌨ button is always visible in the bottom-right corner of the fullscreen window
- The button has a high display priority (HighImportance)
- Button size: 50x50 pixels

### 2. Separate window for the keyboard
- Tapping the ⌨ button opens a separate window with the virtual keyboard
- Keyboard window size: 800x400 pixels
- The window is centered on screen
- The window can be closed by clicking the close button

### 3. Full keyboard layout
- All main keys: letters, digits, symbols
- Function keys: F1-F12
- Arrows: ↑, ↓, ←, →
- Additional keys: Insert, Home, End, Page Up, Page Down, Delete
- Special keys: Enter, Backspace, Tab, Space, Caps Lock
- Toggleable modifiers: Shift, Ctrl, Alt (turn on/off when clicked)

### 4. Sending to the host
- All virtual keyboard keypresses are sent to the remote machine
- Support for single keys and modifier combinations
- Automatic HID code and modifier detection
- Logging of all sent commands

## Technical details

### Files
- `internal/ui/virtual_keyboard.go` - new file implementing the virtual keyboard
- `internal/ui/video_dialogs.go` - updated to integrate with the virtual keyboard

### Data structures
```go
type VirtualKeyboard struct {
    container       *fyne.Container
    keyboard        *fyne.Container
    toggleBtn       *widget.Button
    isVisible       bool
    onKeyPress      func(keyCode int, modifiers int)
    parentWindow    fyne.Window
    keyboardWindow  fyne.Window
}
```

### Main methods
- `NewVirtualKeyboard()` - creates a new keyboard
- `ShowInSeparateWindow()` - shows it in a separate window
- `Hide()` - hides the keyboard
- `handleKeyPress()` - handles keypresses
- `toggleModifier()` - toggles modifiers
- `updateModifierButton()` - updates modifier button appearance
- `sendKeyToRemoteVirtual()` - sends to the remote machine

## Usage

1. Launch the app
2. Connect to USBridge
3. Start video streaming
4. Tap "Fullscreen"
5. Tap the "⌨️ Keyboard" button next to the video controls
6. Use the virtual keyboard to type on the remote machine:
   - Tap modifiers (Ctrl, Alt, Shift) to toggle them on/off
   - Use function keys F1-F12
   - Use arrows to navigate
   - All keypresses are sent to the remote machine
7. Close the keyboard window by clicking the close button

## Advantages

- ✅ Available from the main app window
- ✅ Full keyboard layout with F1-F12 and arrows
- ✅ Modifiers with visual toggling
- ✅ Always sends keypresses to the host
- ✅ Convenient interface in a separate window
- ✅ Logging for debugging
- ✅ Support for key combinations
