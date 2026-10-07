package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// The group check is the one thing standing between "any certificate the
// cluster CA signed" -- which includes every kubelet -- and this API. It is
// worth a test of its own.
func TestAuthorizeByCertificateGroup(t *testing.T) {
	okHandler := func(context.Context, any) (any, error) { return "ok", nil }

	cases := []struct {
		name    string
		org     []string
		issuer  []string
		wantErr bool
	}{
		{name: "system:masters", org: []string{"system:masters"}, wantErr: false},
		{name: "vates:admin", org: []string{"vates:admin"}, wantErr: false},
		{name: "issuer carries it", issuer: []string{"system:masters"}, wantErr: false},
		{name: "node certificate", org: []string{"system:nodes"}, wantErr: true},
		{name: "no organization", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leaf := &x509.Certificate{
				Subject: pkix.Name{CommonName: "test", Organization: tc.org},
				Issuer:  pkix.Name{Organization: tc.issuer},
			}
			ctx := peer.NewContext(context.Background(), &peer.Peer{
				AuthInfo: credentials.TLSInfo{
					State: tls.ConnectionState{
						VerifiedChains: [][]*x509.Certificate{{leaf}},
					},
				},
			})
			_, err := authorize(ctx, nil, &grpc.UnaryServerInfo{}, okHandler)
			if tc.wantErr && err == nil {
				t.Fatalf("authorize accepted %s", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("authorize refused %s: %v", tc.name, err)
			}
		})
	}
}

func TestAuthorizeWithoutPeer(t *testing.T) {
	_, err := authorize(context.Background(), nil, &grpc.UnaryServerInfo{},
		func(context.Context, any) (any, error) { return "ok", nil })
	if err == nil {
		t.Fatal("authorize accepted a call with no peer")
	}
}
