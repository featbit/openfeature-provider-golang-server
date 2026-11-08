package main

import "testing"

func TestListenAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "default", want: "127.0.0.1:1234"},
		{name: "configured", addr: "127.0.0.1:5678", want: "127.0.0.1:5678"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LISTEN_ADDR", tt.addr)
			if got := listenAddr(); got != tt.want {
				t.Errorf("listenAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCalculateCheckoutQuote(t *testing.T) {
	tests := []struct {
		name           string
		promoEnabled   bool
		subtotalCents  int64
		wantPromo      bool
		wantShipping   int64
		wantTotalCents int64
	}{
		{
			name:           "flag disabled",
			subtotalCents:  6500,
			wantShipping:   standardShippingCents,
			wantTotalCents: 6500 + standardShippingCents,
		},
		{
			name:           "enabled below minimum",
			promoEnabled:   true,
			subtotalCents:  freeShippingThresholdCents - 1,
			wantShipping:   standardShippingCents,
			wantTotalCents: freeShippingThresholdCents - 1 + standardShippingCents,
		},
		{
			name:           "enabled at minimum",
			promoEnabled:   true,
			subtotalCents:  freeShippingThresholdCents,
			wantPromo:      true,
			wantTotalCents: freeShippingThresholdCents,
		},
		{
			name:           "enabled above minimum",
			promoEnabled:   true,
			subtotalCents:  6500,
			wantPromo:      true,
			wantTotalCents: 6500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCheckoutQuote(tt.promoEnabled, tt.subtotalCents)
			if got.PromoApplied != tt.wantPromo {
				t.Errorf("PromoApplied = %t, want %t", got.PromoApplied, tt.wantPromo)
			}
			if got.ShippingCents != tt.wantShipping {
				t.Errorf("ShippingCents = %d, want %d", got.ShippingCents, tt.wantShipping)
			}
			if got.TotalCents != tt.wantTotalCents {
				t.Errorf("TotalCents = %d, want %d", got.TotalCents, tt.wantTotalCents)
			}
		})
	}
}
