package gateway

import (
	"testing"

	ma "github.com/multiformats/go-multiaddr"
	glog "github.com/omgolab/go-commons/pkg/log"
)

func TestParseGatewayPath(t *testing.T) {
	log, _ := glog.New(glog.WithFileLogger("test.log"))
	tests := []struct {
		name      string
		path      string
		wantAddrs []string
		wantSvc   string
		shouldErr bool
	}{
		{
			name:      "Single multiaddress with service path",
			path:      "/@//ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4/@/greeter/SayHello",
			wantAddrs: []string{"/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4"},
			wantSvc:   "greeter/SayHello",
			shouldErr: false,
		},
		{
			name:      "Multiple multiaddresses with service path",
			path:      "/@//ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4/@//ip4/1.2.3.4/tcp/9191/@/greeter/SayHello",
			wantAddrs: []string{"/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4", "/ip4/1.2.3.4/tcp/9191"},
			wantSvc:   "greeter/SayHello",
			shouldErr: false,
		},
		{
			name:      "Multiple multiaddresses including IPv6 with service path",
			path:      "/@//ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4/@//ip4/1.2.3.4/tcp/9191/@//ip6/::1/tcp/9292/@/greeter/SayHello",
			wantAddrs: []string{"/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4", "/ip4/1.2.3.4/tcp/9191", "/ip6/::1/tcp/9292"},
			wantSvc:   "greeter/SayHello",
			shouldErr: false,
		},
		{
			name:      "Invalid - No p2p in first multiaddr",
			path:      "/@/ip4/127.0.0.1/tcp/9090/@/greeter/SayHello",
			wantAddrs: nil,
			wantSvc:   "",
			shouldErr: true,
		},
		{
			name:      "Invalid - No service path",
			path:      "/@//ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			wantAddrs: nil,
			wantSvc:   "",
			shouldErr: true,
		},
		{
			name:      "Invalid - Missing method in service path",
			path:      "/@//ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4/@/greeter",
			wantAddrs: []string{"/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4"},
			wantSvc:   "greeter",
			shouldErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			multiaddrsGroups, svc, err := parseGatewayPath(tt.path, log)
			if (err != nil) != tt.shouldErr {
				t.Errorf("parseGatewayPath() error = %v, shouldErr %v", err, tt.shouldErr)
				return
			}
			if tt.shouldErr {
				return
			}

			// Flatten multiaddr groups for comparison
			var addrs []string
			for _, group := range multiaddrsGroups {
				for _, addr := range group {
					addrs = append(addrs, addr.String())
				}
			}

			if len(addrs) != len(tt.wantAddrs) {
				t.Errorf("parseGatewayPath() got %v addresses, want %v", len(addrs), len(tt.wantAddrs))
				return
			}

			for i, addr := range addrs {
				if addr != tt.wantAddrs[i] {
					t.Errorf("parseGatewayPath() addr[%d] = %v, want %v", i, addr, tt.wantAddrs[i])
				}
			}

			if svc != tt.wantSvc {
				t.Errorf("parseGatewayPath() svc = %v, want %v", svc, tt.wantSvc)
			}
		})
	}
}

func TestExtractPeerID(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantID  string
		wantErr bool
	}{
		{
			name:    "Valid multiaddr with peer ID",
			addr:    "/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			wantID:  "12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			wantErr: false,
		},
		{
			name:    "Invalid multiaddr - no peer ID",
			addr:    "/ip4/127.0.0.1/tcp/9090",
			wantID:  "",
			wantErr: true,
		},
		{
			name:    "Invalid multiaddr - empty peer ID",
			addr:    "/ip4/127.0.0.1/tcp/9090/p2p/",
			wantID:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maddr, err := ma.NewMultiaddr(tt.addr)
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("Failed to create multiaddr: %v", err)
				}
				return
			}

			got, err := extractPeerID(maddr)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractPeerID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got.String() != tt.wantID {
				t.Errorf("extractPeerID() = %v, want %v", got, tt.wantID)
			}
		})
	}
}

func TestContainsProtocolInMultiaddr(t *testing.T) {
	tests := []struct {
		name     string
		maddr    string
		protocol string
		want     bool
	}{
		{
			name:     "TCP protocol",
			maddr:    "/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "tcp",
			want:     true,
		},
		{
			name:     "WebSocket protocol",
			maddr:    "/ip4/127.0.0.1/tcp/9091/ws/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "ws",
			want:     true,
		},
		{
			name:     "WebTransport protocol",
			maddr:    "/ip4/127.0.0.1/udp/9092/quic-v1/webtransport/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "webtransport",
			want:     true,
		},
		{
			name:     "WebRTC protocol",
			maddr:    "/ip4/127.0.0.1/udp/9093/webrtc/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "webrtc",
			want:     true,
		},
		{
			name:     "Missing protocol",
			maddr:    "/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "ws",
			want:     false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			maddr, err := ma.NewMultiaddr(tc.maddr)
			if err != nil {
				t.Fatalf("Failed to create multiaddr: %v", err)
			}
			got := containsProtocol(maddr, tc.protocol)
			if got != tc.want {
				t.Errorf("containsProtocol() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractPortFromMultiaddr(t *testing.T) {
	tests := []struct {
		name     string
		maddr    string
		protocol string
		want     string
		wantErr  bool
	}{
		{
			name:     "TCP port",
			maddr:    "/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "tcp",
			want:     "9090",
			wantErr:  false,
		},
		{
			name:     "UDP port",
			maddr:    "/ip4/127.0.0.1/udp/9092/quic-v1/webtransport/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "udp",
			want:     "9092",
			wantErr:  false,
		},
		{
			name:     "Missing protocol",
			maddr:    "/ip4/127.0.0.1/tcp/9090/p2p/12D3KooWRcDTroYkRCArLG69PasPsg26mbG9Pt5NvHjqJ9qfipx4",
			protocol: "udp",
			want:     "",
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			maddr, err := ma.NewMultiaddr(tc.maddr)
			if err != nil {
				t.Fatalf("Failed to create multiaddr: %v", err)
			}
			got, err := extractPort(maddr, tc.protocol)
			if (err != nil) != tc.wantErr {
				t.Errorf("extractPort() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("extractPort() = %v, want %v", got, tc.want)
			}
		})
	}
}
