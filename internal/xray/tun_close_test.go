package xray

import (
	"reflect"
	"testing"

	"github.com/xtls/xray-core/proxy/tun"
)

// TestTunStackLayout guards closeTunStack against changes in xray-core.
func TestTunStackLayout(t *testing.T) {
	f, ok := reflect.TypeOf(tun.Handler{}).FieldByName("stack")
	if !ok || f.Type != reflect.TypeOf((*tun.Stack)(nil)).Elem() {
		t.Fatal("proxy/tun.Handler no longer has a stack field of type Stack; check closeTunStack")
	}
	if tunStack(&tun.Handler{}) != nil {
		t.Fatal("a handler without a stack must yield nil")
	}
}

// TestCloseWithoutTun: closeTunStack is a no-op for configs without TUN.
func TestCloseWithoutTun(t *testing.T) {
	inst, err := Start([]byte(`{"inbounds": [{"listen": "127.0.0.1", "port": 0, "protocol": "socks"}], "outbounds": [{"protocol": "freedom"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.Close(); err != nil {
		t.Fatal(err)
	}
}
