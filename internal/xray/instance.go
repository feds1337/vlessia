package xray

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	cserial "github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
	vless_outbound "github.com/xtls/xray-core/proxy/vless/outbound"
)

// Instance is a running Xray-core.
type Instance struct {
	mu   sync.Mutex
	core *core.Instance
}

// Start builds and starts Xray-core from a JSON config.
func Start(jsonConfig []byte) (*Instance, error) {
	pb, err := loadConfig(jsonConfig)
	if err != nil {
		return nil, fmt.Errorf("ошибка конфига xray: %w", err)
	}
	c, err := core.New(pb)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать xray: %w", err)
	}
	if err := c.Start(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("не удалось запустить xray: %w", err)
	}
	return &Instance{core: c}, nil
}

// loadConfig parses a JSON config into a protobuf core.Config, allowing
// unencrypted VLESS outbounds (security=none) used by public subscriptions.
func loadConfig(jsonConfig []byte) (*core.Config, error) {
	pb, err := serial.LoadJSONConfig(bytes.NewReader(jsonConfig))
	if err == nil || !strings.Contains(err.Error(), "without TLS") {
		return pb, err
	}
	dc, err := serial.DecodeJSONConfig(bytes.NewReader(jsonConfig))
	if err != nil {
		return nil, err
	}
	realAddrs := make(map[int]string)
	for i := range dc.OutboundConfigs {
		ob := &dc.OutboundConfigs[i]
		if ob.Protocol != "vless" || ob.Settings == nil {
			continue
		}
		var raw struct {
			Vnext []map[string]any `json:"vnext"`
		}
		if json.Unmarshal(*ob.Settings, &raw) != nil || len(raw.Vnext) == 0 {
			continue
		}
		addr, _ := raw.Vnext[0]["address"].(string)
		if addr == "" {
			continue
		}
		realAddrs[i] = addr
		raw.Vnext[0]["address"] = "localhost"
		if patched, err := json.Marshal(raw); err == nil {
			rm := json.RawMessage(patched)
			ob.Settings = &rm
		}
	}
	pb, err = dc.Build()
	if err != nil {
		return nil, err
	}
	for i, addr := range realAddrs {
		if i >= len(pb.Outbound) || pb.Outbound[i].ProxySettings == nil {
			continue
		}
		inst, err := pb.Outbound[i].ProxySettings.GetInstance()
		if err != nil {
			continue
		}
		if vc, ok := inst.(*vless_outbound.Config); ok && vc.Vnext != nil {
			vc.Vnext.Address = xnet.NewIPOrDomain(xnet.ParseAddress(addr))
			pb.Outbound[i].ProxySettings = cserial.ToTypedMessage(vc)
		}
	}
	return pb, nil
}

// Close stops Xray-core. It is safe to call more than once.
func (i *Instance) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.core == nil {
		return nil
	}
	tunClosed := closeTunStack(i.core)
	err := i.core.Close()
	if tunClosed && alreadyClosed(err) {
		// The TUN inbound was closed above, so core.Close closing it again
		// reports "file already closed" (seen on Android); that is not a
		// failure.
		err = nil
	}
	i.core = nil
	resetHysteria()
	return err
}

// alreadyClosed reports whether err only says that a file was already
// closed. Xray joins errors into strings, so the text is checked too.
func alreadyClosed(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), os.ErrClosed.Error())
}

// Traffic returns and resets the proxy outbound byte counters. Behind a
// balancing tag there is no "proxy" outbound: the connections are handled
// by the bal-N members of the group, whose counters are summed here.
func (i *Instance) Traffic() (up, down int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.core == nil {
		return 0, 0
	}
	m, ok := i.core.GetFeature(stats.ManagerType()).(stats.Manager)
	if !ok {
		return 0, 0
	}
	read := func(c stats.Counter) int64 {
		if c == nil {
			return 0
		}
		return c.Set(0)
	}
	if c := m.GetCounter("outbound>>>" + TagProxy + ">>>traffic>>>uplink"); c != nil {
		up = read(c)
		down = read(m.GetCounter("outbound>>>" + TagProxy + ">>>traffic>>>downlink"))
		return up, down
	}
	for n := 1; ; n++ {
		tag := fmt.Sprintf("%s-%d", TagBalance, n)
		cu := m.GetCounter("outbound>>>" + tag + ">>>traffic>>>uplink")
		cd := m.GetCounter("outbound>>>" + tag + ">>>traffic>>>downlink")
		if cu == nil && cd == nil {
			return up, down
		}
		up += read(cu)
		down += read(cd)
	}
}
