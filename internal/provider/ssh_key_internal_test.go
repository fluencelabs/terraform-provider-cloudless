package provider

import "testing"

func TestSamePublicKeyBody(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{
			name: "same body, different comment",
			a:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9 igorkonstd3v@gmail.com",
			b:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9 gurinderu@gmail.com",
			want: true,
		},
		{
			name: "same body, one without comment",
			a:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9",
			b:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9 user@host",
			want: true,
		},
		{
			name: "extra whitespace ignored",
			a:    "  ssh-ed25519   AAAAC3NzaC1lZDI1NTE5AAAAIME9   comment ",
			b:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9",
			want: true,
		},
		{
			name: "different body",
			a:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIME9 x",
			b:    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDIFF x",
			want: false,
		},
		{
			name: "different algorithm",
			a:    "ssh-rsa AAAAB3 x",
			b:    "ssh-ed25519 AAAAB3 x",
			want: false,
		},
		{
			name: "malformed missing body",
			a:    "ssh-ed25519",
			b:    "ssh-ed25519",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := samePublicKeyBody(tt.a, tt.b); got != tt.want {
				t.Fatalf("samePublicKeyBody(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
