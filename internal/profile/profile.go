// Package profile describes a single proxy server and parses share links
// (vless://, vmess://, trojan://, ss://) and subscription payloads into it.
package profile

// Server is one outbound endpoint imported from a share link.
type Server struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // vless | vmess | trojan | shadowsocks | hysteria2 | wireguard | amneziawg | openvpn | socks | http
	Address  string `json:"address"`
	Port     int    `json:"port"`

	// Credentials: UUID for vless/vmess, password for trojan/shadowsocks.
	UUID       string `json:"uuid,omitempty"`
	Password   string `json:"password,omitempty"`
	Flow       string `json:"flow,omitempty"`       // vless
	Encryption string `json:"encryption,omitempty"` // vless ("none" or VLESS encryption string)
	AlterID    int    `json:"alterId,omitempty"`    // vmess
	Cipher     string `json:"cipher,omitempty"`     // vmess security / shadowsocks method

	// Transport.
	Network     string `json:"network"` // tcp | ws | grpc | xhttp | httpupgrade
	HeaderType  string `json:"headerType,omitempty"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	Mode        string `json:"mode,omitempty"` // grpc multi/gun, xhttp mode
	Extra       string `json:"extra,omitempty"`

	// Security layer.
	Security    string `json:"security"` // none | tls | reality
	SNI         string `json:"sni,omitempty"`
	ALPN        string `json:"alpn,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	PublicKey   string `json:"publicKey,omitempty"`
	ShortID     string `json:"shortId,omitempty"`
	SpiderX     string `json:"spiderX,omitempty"`
	PinnedSHA   string `json:"pinnedSha256,omitempty"`
	VerifyName  string `json:"verifyName,omitempty"`

	// Hysteria 2.
	Obfs         string `json:"obfs,omitempty"` // "salamander" or empty
	ObfsPassword string `json:"obfsPassword,omitempty"`
	HopPorts     string `json:"hopPorts,omitempty"` // port hopping, e.g. "20000-30000"
	// Insecure means the link asked to skip certificate verification. Xray
	// no longer supports that, so the certificate is pinned on first use.
	Insecure bool `json:"insecure,omitempty"`
	UpMbps   int  `json:"upMbps,omitempty"`
	DownMbps int  `json:"downMbps,omitempty"`

	// WireGuard / AmneziaWG.
	WG *WireGuard `json:"wg,omitempty"`

	// OpenVPN.
	OVPN *OpenVPN `json:"ovpn,omitempty"`

	// Outline: dynamic access key (ssconf://) the server was loaded from,
	// re-fetched on every connect. Prefix is Outline's connection prefix
	// (disguise bytes); Xray cannot send it, so it is kept for reference.
	AccessKeyURL string `json:"accessKeyUrl,omitempty"`
	Prefix       string `json:"prefix,omitempty"`

	// SOCKS5 / HTTP proxies.
	Username string `json:"username,omitempty"`

	SubID string `json:"subId,omitempty"` // owning subscription, empty for manual
	Link  string `json:"link"`            // original share link
}

// WireGuard holds a WireGuard / AmneziaWG client config. Keys are base64,
// as in .conf files. Address/Port of the Server hold the peer endpoint.
type WireGuard struct {
	PrivateKey   string   `json:"privateKey"`
	Addresses    []string `json:"addresses"` // tunnel addresses, CIDR
	DNS          []string `json:"dns,omitempty"`
	MTU          int      `json:"mtu,omitempty"`
	PublicKey    string   `json:"publicKey"`
	PresharedKey string   `json:"presharedKey,omitempty"`
	AllowedIPs   []string `json:"allowedIps,omitempty"`
	// PersistentKeepalive in seconds; AmneziaWG 3 allows a range ("25-35").
	PersistentKeepalive string `json:"persistentKeepalive,omitempty"`
	// AmneziaWG parameters from [Interface] in file order, as written there
	// (Jc, S1, H1, I1, HeaderProtectionKey, RandomTrailers, ...).
	AWG []Param `json:"awg,omitempty"`
}

// OpenVPN holds an .ovpn profile. Address/Port of the Server show the
// remote it connects to.
type OpenVPN struct {
	Config   string `json:"config"`
	NeedAuth bool   `json:"needAuth,omitempty"` // auth-user-pass without stored credentials in the file
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type Param struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// IsUDPOnly reports whether the server cannot be checked with a TCP ping.
func (s *Server) IsUDPOnly() bool {
	return s.WG != nil || s.Protocol == "hysteria2"
}

// IsFullTunnel reports whether the protocol is a layer-3 VPN that is run
// through a local bridge and only makes sense in TUN mode.
func (s *Server) IsFullTunnel() bool {
	return s.WG != nil || s.OVPN != nil
}
