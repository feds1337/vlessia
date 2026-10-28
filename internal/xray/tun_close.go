package xray

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/tun"
	"github.com/xtls/xray-core/transport/internet"
)

// closeTunStack stops the TUN inbound's stack and underlying TUN device
// (such as WindowsTun on Windows) before core.Close tears down outbounds,
// and clears global dialer controllers registered by tun.Handler.Start.
//
// The unexported stack field is also reached by reflection as a safeguard;
// TestTunStackLayout guards against layout changes.
func closeTunStack(c *core.Instance) {
	internet.ControllersLock.Lock()
	internet.Controllers = nil
	internet.ControllersLock.Unlock()

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
	_ = th.Close()
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
