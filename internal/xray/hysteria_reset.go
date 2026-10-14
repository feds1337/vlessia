package xray

import (
	"sync"
	"unsafe"

	"github.com/xtls/xray-core/transport/internet/hysteria"
)

// Xray's Hysteria transport keeps one client per server address in a
// package-level cache for the life of the process and never removes
// entries (its periodic cleanup only closes idle connections). A new Xray
// instance would therefore silently reuse the previous instance's TLS
// settings, auth, obfuscation and socket options — e.g. the adapter binding
// when switching between proxy and TUN mode. resetHysteria empties that
// cache; it is called whenever an instance stops.
//
// The linknamed symbols mirror xray-core v1.260327.0
// (transport/internet/hysteria/dialer.go); TestHysteriaResetLayout guards
// against changes.

// hyClientManager mirrors hysteria.clientManager.
type hyClientManager struct {
	m     map[string]unsafe.Pointer // *hysteria.client
	mutex sync.Mutex
}

//go:linkname hyManager github.com/xtls/xray-core/transport/internet/hysteria.manger
var hyManager *hyClientManager

//go:linkname hyClientStatus github.com/xtls/xray-core/transport/internet/hysteria.(*client).status
func hyClientStatus(c unsafe.Pointer) hysteria.Status

//go:linkname hyClientClose github.com/xtls/xray-core/transport/internet/hysteria.(*client).close
func hyClientClose(c unsafe.Pointer)

func resetHysteria() {
	if hyManager == nil {
		return
	}
	hyManager.mutex.Lock()
	defer hyManager.mutex.Unlock()
	for addr, c := range hyManager.m {
		// close() dereferences the connection, which is nil until the
		// first dial.
		if hyClientStatus(c) != hysteria.StatusUnknown {
			hyClientClose(c)
		}
		delete(hyManager.m, addr)
	}
}
