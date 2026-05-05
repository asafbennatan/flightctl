package domain

import (
	"testing"
)

func TestDigestCVEInvolvedObjectName(t *testing.T) {
	tests := []struct {
		name        string
		imageDigest string
		cveID       string
		want        string
	}{
		{
			name:        "sha256 prefixes lowercased",
			imageDigest: "sha256:ABCDEF0123456789",
			cveID:       "CVE-2024-9999",
			want:        "sha256-abcdef0123456789-cve-2024-9999",
		},
		{
			name:        "colon replaced in digest algorithm",
			imageDigest: "algo:value",
			cveID:       "CVE-2025-1",
			want:        "algo-value-cve-2025-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DigestCVEInvolvedObjectName(tt.imageDigest, tt.cveID)
			if got != tt.want {
				t.Fatalf("DigestCVEInvolvedObjectName(%q,%q) = %q, want %q", tt.imageDigest, tt.cveID, got, tt.want)
			}
		})
	}
}
