package xray

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
)

// Instance is a running Xray-core.
type Instance struct {
	mu   sync.Mutex
	core *core.Instance
}

// Start builds and starts Xray-core from a JSON config.
func Start(jsonConfig []byte) (*Instance, error) {
	pb, err := serial.LoadJSONConfig(bytes.NewReader(jsonConfig))
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

// Close stops Xray-core. It is safe to call more than once.
func (i *Instance) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.core == nil {
		return nil
	}
	closeTunStack(i.core)
	err := i.core.Close()
	i.core = nil
	resetHysteria()
	return err
}

// Traffic returns and resets the proxy outbound byte counters.
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
	if c := m.GetCounter("outbound>>>" + TagProxy + ">>>traffic>>>uplink"); c != nil {
		up = c.Set(0)
	}
	if c := m.GetCounter("outbound>>>" + TagProxy + ">>>traffic>>>downlink"); c != nil {
		down = c.Set(0)
	}
	return up, down
}
