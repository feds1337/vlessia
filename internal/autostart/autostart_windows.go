//go:build windows

// Package autostart starts the client when the user logs in to Windows.
//
// Elevated programs listed under the registry "Run" key are silently skipped
// by Windows, yet TUN mode needs administrator rights. So when the client
// runs elevated, autostart is a Task Scheduler task with "highest
// privileges" (starts elevated, no UAC prompt); otherwise it falls back to
// the per-user Run key, which is enough for system-proxy mode.
package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	name   = "Vlessia"
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	// Arg marks a launch made by autostart.
	Arg = "--autostart"
)

// Mode of the current registration.
const (
	ModeOff  = ""
	ModeTask = "task" // scheduler task, starts elevated
	ModeRun  = "run"  // registry Run key, starts without elevation
)

type Status struct {
	Mode    string `json:"mode"`
	Command string `json:"command"` // registered executable path
}

// Get reports the current registration.
func Get() Status {
	if out, err := schtasks("/query", "/tn", name, "/xml"); err == nil {
		return Status{Mode: ModeTask, Command: taskCommand(out)}
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		if v, _, err := k.GetStringValue(name); err == nil {
			return Status{Mode: ModeRun, Command: runCommand(v)}
		}
	}
	return Status{}
}

// Enable registers exe to start at logon, elevated when possible.
func Enable(exe string) error {
	Disable()
	if windows.GetCurrentProcessToken().IsElevated() {
		return createTask(exe)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, fmt.Sprintf(`"%s" %s`, exe, Arg))
}

// Disable removes both kinds of registration.
func Disable() {
	_, _ = schtasks("/delete", "/tn", name, "/f")
	if k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(name)
		k.Close()
	}
}

func createTask(exe string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	// Defaults of schtasks-created tasks would skip the start on battery
	// and kill the client after 72 hours; both are turned off here.
	task := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Запуск Vlessia при входе в Windows</Description></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled><UserId>` + esc(u.Username) + `</UserId><Delay>PT5S</Delay></LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author"><UserId>` + esc(u.Username) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings><StopOnIdleEnd>false</StopOnIdleEnd><RestartOnIdle>false</RestartOnIdle></IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>5</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>` + esc(exe) + `</Command><Arguments>` + Arg + `</Arguments><WorkingDirectory>` + esc(filepath.Dir(exe)) + `</WorkingDirectory></Exec>
  </Actions>
</Task>`

	// schtasks /xml expects UTF-16 LE with a BOM.
	f, err := os.CreateTemp("", "vlessia-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	u16 := utf16.Encode([]rune(task))
	buf := []byte{0xFF, 0xFE}
	for _, c := range u16 {
		buf = append(buf, byte(c), byte(c>>8))
	}
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if out, err := schtasks("/create", "/tn", name, "/xml", f.Name(), "/f"); err != nil {
		return fmt.Errorf("не удалось создать задание автозапуска: %v %s", err, strings.TrimSpace(out))
	}
	return nil
}

var commandRe = regexp.MustCompile(`<Command>([^<]*)</Command>`)

func taskCommand(xmlText string) string {
	m := commandRe.FindStringSubmatch(xmlText)
	if m == nil {
		return ""
	}
	var s string
	if err := xml.Unmarshal([]byte("<x>"+m[1]+"</x>"), &s); err != nil {
		return m[1]
	}
	return s
}

func runCommand(v string) string {
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : end+1]
		}
	}
	return strings.Fields(v)[0]
}

// schtasks runs schtasks.exe without a console window. /query /xml prints
// UTF-16, which is decoded here.
func schtasks(args ...string) (string, error) {
	cmd := exec.Command("schtasks", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	return decodeOutput(out), err
}

func decodeOutput(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}
	// UTF-16 output has a zero high byte for ASCII characters.
	if len(b) >= 2 && b[1] == 0 {
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}
