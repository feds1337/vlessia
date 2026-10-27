package xray

import (
	"sync"
	"unsafe"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

// Xray's Hysteria transport keeps clients in a package-level cache and only
// cleans them up on a periodic timer. resetHysteria forces immediate cleanup
// and empties that cache whenever an instance stops.
//
// The linknamed symbols mirror xray-core v26.10.21
// (transport/internet/hysteria/dialer.go); TestHysteriaResetLayout guards
// against changes.

type hyDialerConf struct {
	xnet.Destination
	*internet.MemoryStreamConfig
}

// hyClientManager mirrors hysteria.clientManager.
type hyClientManager struct {
	sync.RWMutex
	m map[hyDialerConf]unsafe.Pointer // *hysteria.client
}

//go:linkname hyManager github.com/xtls/xray-core/transport/internet/hysteria.manager
var hyManager *hyClientManager

//go:linkname hyClientClean github.com/xtls/xray-core/transport/internet/hysteria.(*client).clean
func hyClientClean(c unsafe.Pointer, force bool)

func resetHysteria() {
	if hyManager == nil {
		return
	}
	hyManager.Lock()
	defer hyManager.Unlock()
	for k, c := range hyManager.m {
		hyClientClean(c, true)
		delete(hyManager.m, k)
	}
}
