package xray

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/tun"
)

// Xray's TUN inbound (proxy/tun.Handler in xray-core v1.260327.0) has no
// Close: when an instance stops, the gVisor stack behind the inbound and
// the goroutine reading the TUN device keep running. On Android the reader
// then sits on a descriptor number that is already closed — holding the
// VPN interface up until the next packet arrives, or reading from whatever
// reuses that number, such as the next connection's TUN device.
//
// closeTunStack stops that stack. The unexported field is reached by
// reflection; TestTunStackLayout guards against changes.
func closeTunStack(c *core.Instance) {
	m, ok := c.GetFeature(inbound.ManagerType()).(inbound.Manager)
	if !ok {
		return
	}
	h, err := m.GetHandler(context.Background(), TagTun)
	if err != nil {
		return // no TUN inbound in this config
	}
	g, ok := h.(interface{ GetInbound() proxy.Inbound })
	if !ok {
		return
	}
	th, ok := g.GetInbound().(*tun.Handler)
	if !ok || th == nil {
		return
	}
	if s := tunStack(th); s != nil {
		_ = s.Close()
	}
}

// tunStack returns the handler's stack, or nil.
func tunStack(th *tun.Handler) tun.Stack {
	f := reflect.ValueOf(th).Elem().FieldByName("stack")
	if !f.IsValid() || f.Type() != reflect.TypeOf((*tun.Stack)(nil)).Elem() {
		return nil
	}
	s, _ := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(tun.Stack)
	return s
}
