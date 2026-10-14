package main

// Tray is not used on macOS: fyne/systray needs the main thread, which
// Wails owns there. The window minimises to the Dock instead and the app
// menu provides Quit.
type Tray struct{}

func StartTray(*App) *Tray         { return &Tray{} }
func (*Tray) Update(Status)        {}
func (*Tray) SetUpdate(UpdateInfo) {}
func (*Tray) Stop()                {}
